package dcrlibwallet

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"

	"decred.org/dcrwallet/v2/errors"
	w "decred.org/dcrwallet/v2/wallet"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"
)

const (
	DcrdataMainnetHost = "https://dcrdata.decred.org/api/agendas"
	DcrdataTestnetHost = "https://testnet.decred.org/api/agendas"

	AgendaStatusUpcoming   = "upcoming"
	AgendaStatusInProgress = "in progress"
	AgendaStatusFinished   = "finished"
)

// SetVoteChoice sets a voting choice for the specified agenda. If a ticket
// hash is provided, the voting choice is also updated with the VSP controlling
// the ticket. If a ticket hash isn't provided, the vote choice is saved to the
// local wallet database and the VSPs controlling all unspent, unexpired tickets
// are updated to use the specified vote choice.
func (wallet *Wallet) SetVoteChoice(agendaID, choiceID, hash string, passphrase []byte) error {
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

	var vspPreferenceUpdateSuccess bool
	defer func() {
		if !vspPreferenceUpdateSuccess {
			// Updating the agenda voting preference with the vsp failed,
			// revert the locally saved voting preference for the agenda.
			_, revertError := wallet.Internal().SetAgendaChoices(ctx, ticketHash, currentChoice)
			if revertError != nil {
				log.Errorf("unable to revert locally saved voting preference: %v", revertError)
			}
		}
	}()

	// If a ticket hash is provided, set the specified vote choice with
	// the VSP associated with the provided ticket. Otherwise, set the
	// vote choice with the VSPs associated with all "votable" tickets.
	ticketHashes := make([]*chainhash.Hash, 0)
	if ticketHash != nil {
		ticketHashes = append(ticketHashes, ticketHash)
	} else {
		err = wallet.Internal().ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
			ticketHashes = append(ticketHashes, hash)
			return nil
		})
		if err != nil {
			return fmt.Errorf("unable to fetch hashes for all unspent, unexpired tickets: %v", err)
		}
	}

	// Never return errors from this for loop, so all tickets are tried.
	// The first error will be returned to the caller.
	var firstErr error
	for _, tHash := range ticketHashes {
		vspTicketInfo, err := wallet.Internal().VSPTicketInfo(ctx, tHash)
		if err != nil {
			// Ignore NotExist error, just means the ticket is not
			// registered with a VSP, nothing more to do here.
			if firstErr == nil && !errors.Is(err, errors.NotExist) {
				firstErr = err
			}
			continue // try next tHash
		}

		// Update the vote choice for the ticket with the associated VSP.
		vspClient, err := wallet.VSPClient(vspTicketInfo.Host, vspTicketInfo.PubKey)
		if err != nil && firstErr == nil {
			firstErr = err
			continue // try next tHash
		}
		err = vspClient.SetVoteChoice(ctx, tHash, newChoice)
		if err != nil && firstErr == nil {
			firstErr = err
			continue // try next tHash
		}
	}

	vspPreferenceUpdateSuccess = firstErr == nil
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

	// Fetch high level agenda detail form dcrdata api.
	var agendaTagged []AgendaTagged
	host := DcrdataMainnetHost
	if wallet.chainParams.Net == wire.TestNet3 {
		host = DcrdataTestnetHost
	}
	err = unmarshal(host, &agendaTagged)
	if err != nil {
		return nil, err
	}

	agendas := make([]*Agenda, len(deployments))
	var status string
	for i := range deployments {
		d := &deployments[i]

		votingPreference := "abstain" // assume abstain, if we have the saved pref, it'll be updated below
		for c := range choices {
			if choices[c].AgendaID == d.Vote.Id {
				votingPreference = choices[c].ChoiceID
				break
			}
		}

		for j := range agendaTagged {
			if agendaTagged[j].Name == d.Vote.Id {
				status = agendaTagged[j].Status
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
			Status:           status,
		}
	}

	if newestFirst {
		sort.Slice(agendas, func(i, j int) bool {
			return agendas[i].StartTime > agendas[j].StartTime
		})
	}
	return agendas, nil
}

func unmarshal(host string, target interface{}) error {
	res, err := http.Get(host)
	if err != nil {
		return err
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(body, target)
	if err != nil {
		return err
	}

	return nil
}
