package dcrlibwallet

import (
	"fmt"
	"sort"

	w "decred.org/dcrwallet/v2/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
)

// SetVoteChoice sets a voting choice for the specified agenda. If a ticket
// hash is provided, the voting choice is also updated with the VSP controlling
// the ticket. If a ticket hash isn't provided, the vote choice is set for all
// tickets controlled by the specified VSP.
func (wallet *Wallet) SetVoteChoice(vspHost string, vspPubKey []byte, agendaID, choiceID, hash string, passphrase []byte) error {
	if vspHost == "" && hash == "" {
		return fmt.Errorf("request requires either a vsp host or a ticket hash")
	}

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

	// The wallet will need to be unlocked to sign the API
	// request(s) for setting this vote choice with the VSP.
	err := wallet.UnlockWallet(passphrase)
	if err != nil {
		return translateError(err)
	}
	defer wallet.LockWallet()

	ctx := wallet.shutdownContext()

	// get choices
	choices, _, err := wallet.Internal().AgendaChoices(ctx, ticketHash) // returns saved prefs for current agendas
	if err != nil {
		return err
	}

	currentChoice := w.AgendaChoice{
		AgendaID: agendaID,
		ChoiceID: "abstain", // default to abstain as current choice if not found in wallet
	}

	for i := range choices {
		if choices[i].AgendaID == agendaID {
			currentChoice.ChoiceID = choices[i].ChoiceID
			break
		}
	}

	newChoice := w.AgendaChoice{
		AgendaID: agendaID,
		ChoiceID: choiceID,
	}

	_, err = wallet.Internal().SetAgendaChoices(ctx, ticketHash, newChoice)
	if err != nil {
		return err
	}

	// If ticket hash is provided, then set vote choice for the selected ticket
	// with the associated vsp.
	if ticketHash != nil {
		vspTicketInfo, err := wallet.Internal().VSPTicketInfo(ctx, ticketHash)
		if err != nil {
			return err
		}
		// Update the vote choice for the ticket with the vsp
		vspClient, err := wallet.VSPClient(vspTicketInfo.Host, vspTicketInfo.PubKey)
		if err != nil {
			return err
		}
		err = vspClient.SetVoteChoice(ctx, ticketHash, newChoice)
		if err != nil {
			// Updating the agenda voting preference with the vsp failed,
			// revert the locally saved voting preference for the agenda.
			_, revertError := wallet.Internal().SetAgendaChoices(ctx, ticketHash, currentChoice)
			if revertError != nil {
				log.Errorf("unable to revert locally saved voting preference: %v", revertError)
			}
			return err
		}
	}

	vspClient, err := wallet.VSPClient(vspHost, vspPubKey)
	if err != nil {
		return err
	}

	// Ticket hash wasn't provided, therefore set vote choice for all tickets
	// belonging to the selected wallet and controlled by the specififed VSP
	var firstErr error
	vspClient.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		// Never return errors here, so all tickets are tried.
		// The first error will be returned to the user.
		err := vspClient.SetVoteChoice(ctx, hash, newChoice)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return nil
	})

	if firstErr != nil {
		// Updating the agenda voting preference with the vsp failed, therefore
		// revert the locally saved voting preference for the agenda.
		_, revertError := wallet.Internal().SetAgendaChoices(ctx, ticketHash, currentChoice)
		if revertError != nil {
			log.Errorf("unable to revert locally saved voting preference: %v", revertError)
		}
	}

	return firstErr
}

// AllVoteAgendas returns all agendas of all stake versions for the active
// network and this version of the software. Also returns any saved vote
// preferences for the agendas of the current stake version. Vote preferences
// for older agendas cannot currently be retrieved.
func (wallet *Wallet) AllVoteAgendas(hash string, newestFirst bool) ([]*Agenda, error) {
	if wallet.chainParams.Deployments == nil {
		return nil, nil // no agendas to return
	}

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return nil, fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

	ctx := wallet.shutdownContext()
	choices, _, err := wallet.Internal().AgendaChoices(ctx, ticketHash) // returns saved prefs for current agendas
	if err != nil {
		return nil, err
	}

	// Check for all agendas from the intital stake version to the
	// current stake version, in order to fetch legacy agendas.
	deployments := make([]chaincfg.ConsensusDeployment, 0)
	var i uint32
	for i = 1; i <= voteVersion(wallet.chainParams); i++ {
		deployments = append(deployments, wallet.chainParams.Deployments[i]...)
	}

	agendas := make([]*Agenda, len(deployments))
	for i := range deployments {
		d := &deployments[i]

		votingPreference := "abstain" // assume abstain, if we have the saved pref, it'll be updated below
		for c := range choices {
			if choices[c].AgendaID == d.Vote.Id {
				votingPreference = choices[c].ChoiceID
				break
			}
		}

		agendas[i] = &Agenda{
			AgendaID:         d.Vote.Id,
			Description:      d.Vote.Description,
			Mask:             uint32(d.Vote.Mask),
			Choices:          d.Vote.Choices,
			VotingPreference: votingPreference,
			StartTime:        int64(d.StartTime),
			ExpireTime:       int64(d.ExpireTime),
		}
	}

	if newestFirst {
		sort.Slice(agendas, func(i, j int) bool {
			return agendas[i].StartTime > agendas[j].StartTime
		})
	}
	return agendas, nil
}
