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

// GetVoteChoices returns configured vote preferences for each agenda of the
// latest supported stake version for the wallet with the specified walletID.
func (mw *MultiWallet) GetVoteChoices(hash string, walletID int) (*GetVoteChoicesResult, error) {
	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return nil, errors.New(ErrNotExist)
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
		Choices: make([]AgendaVoteChoice, len(agendas)),
	}

	ctx := wallet.shutdownContext()
	choices, _, err := wal.AgendaChoices(ctx, ticketHash)
	if err != nil {
		return nil, err
	}

	for i := range choices {
		agenda := mw.agenda(agendas, choices[i].AgendaID)
		resp.Choices[i] = AgendaVoteChoice{
			AgendaID:          choices[i].AgendaID,
			AgendaDescription: agenda.Vote.Description,
			ChoiceID:          choices[i].ChoiceID,
			ChoiceDescription: "", // Set below
		}
		for _, choice := range agenda.Vote.Choices {
			if choices[i].ChoiceID == choice.Id {
				resp.Choices[i].ChoiceDescription = choice.Description
				break
			}
		}
	}

	return resp, nil
}

func (mw *MultiWallet) agenda(agendas []chaincfg.ConsensusDeployment, agendaID string) *chaincfg.ConsensusDeployment {
	for _, agenda := range agendas {
		if agenda.Vote.Id == agendaID {
			return &agenda
		}
	}

	return nil
}

// SetVoteChoice sets a voting choice for the specified agenda using the
// specified ticket hash controlled by a VSP.
//
// If a ticket hash isn't provided, the vote choice is set for all tickets
// controlled by the set VSP.
func (mw *MultiWallet) SetVoteChoice(walletID int, vspHost string, vspPubKey []byte, agendaID, choiceID, hash string, passphrase []byte) error {
	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrNotExist)
	}
	wal := wallet.Internal()

	if vspHost == "" {
		return fmt.Errorf("request requires a vspHost but no vspHost was provided")
	}

	// Test to ensure the passphrase is correct, and the voting is authorized.
	err := wallet.UnlockWallet(passphrase)
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

	vspClient, err := wallet.VSPClient(vspHost, vspPubKey)
	if err != nil {
		return err
	}

	// If ticket hash is provided, then set vote choice for the selected ticket.
	if ticketHash != nil {
		vspTicketInfo, err := wal.VSPTicketInfo(ctx, ticketHash)
		if err != nil {
			return err
		}
		// Register the provided ticket hash with the VSP client.
		vspClient, err = wallet.VSPClient(vspTicketInfo.Host, vspTicketInfo.PubKey)
		if err != nil {
			return err
		}
		err = vspClient.SetVoteChoice(ctx, ticketHash, choice)
		return err
	}

	// Ticket hash wasn't provided, therefore set vote choice for all tickets
	// belonging to the selected wallet and controlled by the specififed VSP
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

// GetAllAgendasForWallet returns all agendas through out the various
// stake versions for the active network and this version of the software,
// and all agendas defined by it for a selected wallet.
func (mw *MultiWallet) GetAllAgendasForWallet(walletID int, newestFirst bool) (uint32, []*Agenda, error) {
	wallet := mw.WalletWithID(walletID)
	version, deployments := getAllAgendas(wallet.chainParams)
	agendas := make([]*Agenda, len(deployments))

	voteChoicesResult, err := mw.GetVoteChoices("", wallet.ID)
	if err != nil {
		return 0, nil, errors.Errorf("error getting voteChoicesResult: %s", err.Error())
	}

	for i := range deployments {
		d := &deployments[i]

		var votingPreference string
		for j := range voteChoicesResult.Choices {
			if voteChoicesResult.Choices[j].AgendaID == d.Vote.Id {
				votingPreference = voteChoicesResult.Choices[j].ChoiceID
			}
		}

		// If votingPreference is empty, it means the wallet didn't participate
		// in the voting, and the votingPreference can default to abstain.
		if votingPreference == "" {
			votingPreference = "abstain"
		}

		// Get agenda status.
		currentTime := time.Now().Unix()
		var status string
		if currentTime > int64(d.ExpireTime) {
			status = "Finished"
		} else if currentTime > int64(d.StartTime) && currentTime < int64(d.ExpireTime) {
			status = "In progress"
		} else if currentTime > int64(d.StartTime) {
			status = "Upcoming"
		}

		agendas[i] = &Agenda{
			AgendaID:         d.Vote.Id,
			Description:      d.Vote.Description,
			Mask:             uint32(d.Vote.Mask),
			Choices:          make([]*VoteChoice, len(d.Vote.Choices)),
			VotingPreference: votingPreference,
			StartTime:        int64(d.StartTime),
			ExpireTime:       int64(d.ExpireTime),
			Status:           status,
		}
		for j := range d.Vote.Choices {
			choice := &d.Vote.Choices[j]
			agendas[i].Choices[j] = &VoteChoice{
				Id:          choice.Id,
				Description: choice.Description,
				Bits:        uint32(choice.Bits),
				IsAbstain:   choice.IsAbstain,
				IsNo:        choice.IsNo,
			}
		}
	}

	if newestFirst {
		sort.Sort(ByStartTime(agendas))
	}
	return version, agendas, nil
}

func getAllAgendas(params *chaincfg.Params) (version uint32, agendas []chaincfg.ConsensusDeployment) {
	version = voteVersion(params)
	if params.Deployments == nil {
		return version, nil
	}

	var i uint32
	allAgendas := make([]chaincfg.ConsensusDeployment, 0)
	// Check for all agendas from the intital stake version to the current stake version,
	// in order to fetch legacy agendas.
	for i = 1; i <= version; i++ {
		currentAgendas := params.Deployments[i]
		allAgendas = append(allAgendas, currentAgendas...)
	}
	return version, allAgendas
}
