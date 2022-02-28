package dcrlibwallet

import (
	"fmt"
	"sort"
	"time"

	w "decred.org/dcrwallet/v2/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
)

func agenda(agendas []chaincfg.ConsensusDeployment, agendaID string) *chaincfg.ConsensusDeployment {
	for _, agenda := range agendas {
		if agenda.Vote.Id == agendaID {
			return &agenda
		}
	}

	return nil
}

// SetVoteChoice sets a voting choice for the specified agenda. If a ticket
// hash is provided, the voting choice is also updated with the VSP controlling
// the ticket. If a ticket hash isn't provided, the vote choice is set for all
// tickets controlled by the specified VSP.
func (wallet *Wallet) SetVoteChoice(vspHost string, vspPubKey []byte, agendaID, choiceID, hash string, passphrase []byte) error {
	wal := wallet.Internal()

	if vspHost == "" && hash == "" {
		return fmt.Errorf("request requires either a vsp host or a ticket hash")
	}

	// Test to ensure the passphrase is correct, before setting a voting
	// preference.
	err := wallet.UnlockWallet(passphrase)
	if err != nil {
		return translateError(err)
	}
	wallet.LockWallet()

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

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

	_, err = wal.SetAgendaChoices(ctx, ticketHash, newChoice)
	if err != nil {
		return err
	}

	// If ticket hash is provided, then set vote choice for the selected ticket
	// with the associated vsp.
	if ticketHash != nil {
		vspTicketInfo, err := wal.VSPTicketInfo(ctx, ticketHash)
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
			// Updating the agenda voting preference with the vsp failed, therefore
			// revert the locally saved voting preference for the agenda.
			_, err = wal.SetAgendaChoices(ctx, ticketHash, currentChoice)
			if err != nil {
				return err
			}
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
			// Updating the agenda voting preference with the vsp failed, therefore
			// revert the locally saved voting preference for the agenda.
			_, err = wal.SetAgendaChoices(ctx, ticketHash, currentChoice)
			firstErr = err
		}
		return nil
	})

	return firstErr
}

// AllVoteAgendas returns all agendas through out the various
// stake versions for the active network and this version of the software,
// as well as returns the vote preferences for the current version,
// and all agendas defined by it for a selected wallet.
func (wallet *Wallet) AllVoteAgendas(hash string, newestFirst bool) (uint32, []*Agenda, error) {
	wal := wallet.Internal()

	version, deployments := getAllAgendas(wallet.chainParams)
	agendas := make([]*Agenda, len(deployments))

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return 0, nil, fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

	agendaVoteChoices := make([]AgendaVoteChoice, len(deployments))

	ctx := wallet.shutdownContext()
	choices, _, err := wal.AgendaChoices(ctx, ticketHash)
	if err != nil {
		return 0, nil, err
	}

	for i := range deployments {

		for j := range choices {
			agenda := agenda(deployments, choices[j].AgendaID)
			agendaVoteChoices[j] = AgendaVoteChoice{
				AgendaID:          choices[j].AgendaID,
				AgendaDescription: agenda.Vote.Description,
				ChoiceID:          choices[j].ChoiceID,
				ChoiceDescription: "", // Set below
			}
			for _, choice := range agenda.Vote.Choices {
				if choices[j].ChoiceID == choice.Id {
					agendaVoteChoices[j].ChoiceDescription = choice.Description
					break
				}
			}
		}

		d := &deployments[i]

		var votingPreference string
		for k := range agendaVoteChoices {
			if agendaVoteChoices[k].AgendaID == d.Vote.Id {
				votingPreference = agendaVoteChoices[k].ChoiceID
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
		sort.Slice(agendas, func(i, j int) bool {
			return agendas[i].StartTime > agendas[j].StartTime
		})
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
