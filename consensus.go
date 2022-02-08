package dcrlibwallet

import (
	"fmt"
	"sort"
	"time"

	"decred.org/dcrwallet/v2/errors"
	w "decred.org/dcrwallet/v2/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
)

// GetVoteChoices returns configured vote preferences for each agenda of the latest
// supported stake version for the wallet with the specified walletID
func (mw *MultiWallet) GetVoteChoices(hash string, walletID int) (*GetVoteChoicesResult, error) {
	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return nil, fmt.Errorf("request requires a wallet but wallet has not loaded yet")
	}
	wal := wallet.Internal()

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return nil, fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

	version, agendas := getAllAgendas(wal.ChainParams())
	resp := &GetVoteChoicesResult{
		Version: version,
		Choices: make([]VoteChoice, len(agendas)),
	}

	ctx := wallet.shutdownContext()
	choices, _, err := wal.AgendaChoices(ctx, ticketHash)
	if err != nil {
		return nil, err
	}

	for i := range choices {
		agenda := mw.agenda(walletID, choices[i].AgendaID)
		resp.Choices[i] = VoteChoice{
			AgendaID:          choices[i].AgendaID,
			AgendaDescription: agenda.Vote.Description,
			ChoiceID:          choices[i].ChoiceID,
			ChoiceDescription: "", // Set below
		}
		for j := range agendas[i].Vote.Choices {
			if choices[i].ChoiceID == agendas[i].Vote.Choices[j].Id {
				resp.Choices[i].ChoiceDescription = agendas[i].Vote.Choices[j].Description
				break
			}
		}
	}

	return resp, nil
}

func (mw *MultiWallet) agenda(walletID int, agendaID string) *chaincfg.ConsensusDeployment {
	wallet := mw.WalletWithID(walletID)
	wal := wallet.Internal()

	_, agendas := getAllAgendas(wal.ChainParams())

	for _, agenda := range agendas {
		if agenda.Vote.Id == agendaID {
			return &agenda
		}
	}

	return nil
}

// SetVoteChoice sets a voting choice for an agenda using the set voting preference.
// The voting preference is first saved locally, to the database.
//
// However, if a VSP host is configured in the application settings, the voting
// preferences will also be set with the VSP.
func (mw *MultiWallet) SetVoteChoice(walletID int, vspHost string, vspPubKey []byte, agendaID, choiceID, hash string, passphrase []byte) error {
	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return fmt.Errorf("request requires a wallet but wallet has not loaded yet")
	}
	wal := wallet.Internal()

	err := wallet.UnlockWallet([]byte(passphrase))
	if err != nil {
		return translateError(err)
	}
	defer wallet.LockWallet()

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

	choice := w.AgendaChoice{
		AgendaID: agendaID,
		ChoiceID: choiceID,
	}

	ctx := wallet.shutdownContext()
	_, err = wal.SetAgendaChoices(ctx, ticketHash, choice)
	if err != nil {
		return err
	}

	if vspHost == "" {
		return fmt.Errorf("request requires a vspHost but no vspHost was provided")
	}

	vspClient, err := wallet.VSPClient(vspHost, vspPubKey)
	if err != nil && !errors.Is(err, errors.NotExist) {
		return err
	}

	// If ticket hash is provided, then set vote choice for the selected ticket
	if ticketHash != nil {
		vspTicketInfo, err := wal.VSPTicketInfo(ctx, ticketHash)
		if err != nil {
			return err
		}
		// Register the provided ticket hash with the VSP client
		vspClient, err = wallet.VSPClient(vspTicketInfo.Host, vspTicketInfo.PubKey)
		if err != nil && !errors.Is(err, errors.NotExist) {
			return err
		}
		err = vspClient.SetVoteChoice(ctx, ticketHash, choice)
		return err
	}

	// Ticket hash wasn't provided, therefore set vote choice for all tickets
	// belonging to the selected wallet and controlled by the set VSP
	var firstErr error
	vspClient.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		// Never return errors here, so all tickets are tried.
		// The first error will be returned to the user.
		err := vspClient.SetVoteChoice(ctx, hash, choice)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return nil
	})

	return firstErr
}

// GetAllAgendasForWallet returns all agendas through out the various stake versions for the active network and
// this version of the software, and all agendas defined by it for a selected wallet.
func (mw *MultiWallet) GetAllAgendasForWallet(walletID int, newestFirst bool) (*AgendasResponse, error) {
	wallet := mw.WalletWithID(walletID)
	version, deployments := getAllAgendas(wallet.chainParams)
	resp := &AgendasResponse{
		Version: version,
		Agendas: make([]*Agenda, len(deployments)),
	}

	voteChoicesResult, err := mw.GetVoteChoices("", wallet.ID)
	if err != nil {
		return nil, errors.Errorf("error getting voteChoicesResult: %s", err.Error())
	}

	for i := range deployments {
		d := &deployments[i]

		var votingPreference string
		for j := range voteChoicesResult.Choices {
			if voteChoicesResult.Choices[j].AgendaID == d.Vote.Id {
				votingPreference = voteChoicesResult.Choices[j].ChoiceID
			}
		}

		// if votingPreference is empty, it means the wallet didn't participate
		// in the voting, and the votingPreference can default to abstain
		if votingPreference == "" {
			votingPreference = "abstain"
		}

		// get agenda status
		currentTime := time.Now().Unix()
		var status string
		if currentTime > int64(d.ExpireTime) {
			status = "Finished"
		} else if currentTime > int64(d.StartTime) && currentTime < int64(d.ExpireTime) {
			status = "In progress"
		} else if currentTime > int64(d.StartTime) {
			status = "Upcoming"
		}

		resp.Agendas[i] = &Agenda{
			AgendaID:         d.Vote.Id,
			Description:      d.Vote.Description,
			Mask:             uint32(d.Vote.Mask),
			Choices:          make([]*Choice, len(d.Vote.Choices)),
			VotingPreference: votingPreference,
			StartTime:        int64(d.StartTime),
			ExpireTime:       int64(d.ExpireTime),
			Status:           status,
		}
		for j := range d.Vote.Choices {
			choice := &d.Vote.Choices[j]
			resp.Agendas[i].Choices[j] = &Choice{
				Id:          choice.Id,
				Description: choice.Description,
				Bits:        uint32(choice.Bits),
				IsAbstain:   choice.IsAbstain,
				IsNo:        choice.IsNo,
			}
		}
	}

	if newestFirst {
		sort.Sort(ByStartTime(resp.Agendas))
	}
	return resp, nil
}

func getAllAgendas(params *chaincfg.Params) (version uint32, agendas []chaincfg.ConsensusDeployment) {
	version = voteVersion(params)
	if params.Deployments == nil {
		return version, nil
	}

	var i uint32
	allAgendas := make([]chaincfg.ConsensusDeployment, 0)
	// check for all agendas from the intital stake version to the current stake version,
	// in order to fetch legacy agendas
	for i = 1; i <= version; i++ {
		currentAgendas := params.Deployments[i]
		for j := 0; j < len(currentAgendas); j++ {
			agenda := currentAgendas[j]
			allAgendas = append(allAgendas, agenda)
		}
	}
	return version, allAgendas
}
