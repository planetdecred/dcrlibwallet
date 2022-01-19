package dcrlibwallet

import (
	"context"
	"fmt"
	"sort"

	"decred.org/dcrwallet/v2/errors"
	w "decred.org/dcrwallet/v2/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
)

type Consensus struct {
	mwRef *MultiWallet
	ctx   context.Context
}

func newConsensus(mwRef *MultiWallet) (*Consensus, error) {
	c := &Consensus{
		mwRef: mwRef,
	}

	return c, nil
}

// GetVoteChoices handles a getvotechoices request by returning configured vote
// preferences for each agenda of the latest supported stake version.
func (c *Consensus) GetVoteChoices(ctx context.Context, hash string, walletID int) (*GetVoteChoicesResult, error) {
	wallet := c.mwRef.WalletWithID(walletID)
	wal := wallet.Internal()
	if wal == nil {
		return nil, fmt.Errorf("request requires a wallet but wallet has not loaded yet")
	}

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

	choices, _, err := wal.AgendaChoices(ctx, ticketHash)
	if err != nil {
		return nil, err
	}

	for i := range choices {
		resp.Choices[i] = VoteChoice{
			AgendaID:          choices[i].AgendaID,
			AgendaDescription: agendas[i].Vote.Description,
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

// SetVoteChoice handles a setvotechoice request by modifying the preferred
// choice for a voting agenda.
//
// If a VSP host is configured in the application settings, the voting
// preferences will also be set with the VSP.
func (c *Consensus) SetVoteChoice(walletID int, vspHost, agendaID, choiceID, hash, passphrase string) error {
	wallet := c.mwRef.WalletWithID(walletID)
	wal := wallet.Internal()
	if wal == nil {
		return fmt.Errorf("request requires a wallet but wallet has not loaded yet")
	}

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

	var ctx context.Context
	_, err = wal.SetAgendaChoices(ctx, ticketHash, choice)
	if err != nil {
		return err
	}

	println("[][][][] A")
	_, err = c.GetAllAgendasForWallet(walletID, false)
	if err != nil {
		return err
	}
	if vspHost == "" {
		return nil
	}
	println("[][][][] B")
	// vspClient, err := loader.LookupVSP(vspHost)
	// if err != nil {
	// 	return err
	// }
	// var c.mwRef MultiWallet
	vsp, err := c.mwRef.NewVSPClient(vspHost, walletID, 0)
	// vspClient, err := LookupVSP(vspHost)
	if err != nil {
		return err
	}
	if ticketHash != nil {
		err = vsp.SetVoteChoice(ctx, ticketHash, choice)
		return err
	}
	println("[][][][] 1")
	var firstErr error
	vsp.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		// Never return errors here, so all tickets are tried.
		// The first error will be returned to the user.
		println("[][][][] 2")
		err := vsp.SetVoteChoice(ctx, hash, choice)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		println("[][][][] 3")
		_, err = c.GetAllAgendasForWallet(walletID, false)
		if err != nil {
			return err
		}
		println("[][][][] 4")
		return nil
	})
	// _, err = c.mwRef.GetAllAgendas(walletID)
	// if err != nil {
	// 	return err
	// }
	return firstErr
}

// GetAllAgendasForWallet returns all agendas through out the various stake versions for the active network and
// this version of the software, and all agendas defined by it using the walletID.
func (c *Consensus) GetAllAgendasForWallet(walletID int, newestFirst bool) (*AgendasResponse, error) {
	wallet := c.mwRef.WalletWithID(walletID)
	version, deployments := getAllAgendas(wallet.chainParams)
	resp := &AgendasResponse{
		Version: version,
		Agendas: make([]*Agenda, len(deployments)),
	}

	var ctx context.Context
	voteChoicesResult, err := c.GetVoteChoices(ctx, "", wallet.ID)
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

		// if voting prefrence is nil, it means the wallet didn't participate
		// in the voting, and the preference can be set to nil
		if votingPreference == "" {
			votingPreference = "abstain"
		}

		resp.Agendas[i] = &Agenda{
			AgendaID:         d.Vote.Id,
			Description:      d.Vote.Description,
			Mask:             uint32(d.Vote.Mask),
			Choices:          make([]*Choice, len(d.Vote.Choices)),
			VotingPreference: votingPreference,
			StartTime:        int64(d.StartTime),
			ExpireTime:       int64(d.ExpireTime),
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
	for i = 1; i <= version; i++ {
		currentAgendas := params.Deployments[i]
		for j := 0; j < len(currentAgendas); j++ {
			agenda := currentAgendas[j]
			allAgendas = append(allAgendas, agenda)
		}
	}
	return version, allAgendas
}
