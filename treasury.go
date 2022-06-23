package dcrlibwallet

import (
	"encoding/hex"
	"fmt"

	"decred.org/dcrwallet/v2/errors"

	"github.com/decred/dcrd/blockchain/stake/v4"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Sanctioned Politeia keys.
var MainnetPiKeys = [...]string{"03f6e7041f1cf51ee10e0a01cd2b0385ce3cd9debaabb2296f7e9dee9329da946c",
	"0319a37405cb4d1691971847d7719cfce70857c0f6e97d7c9174a3998cf0ab86dd"}
var TestnetPiKeys = [...]string{"03beca9bbd227ca6bb5a58e03a36ba2b52fff09093bd7a50aee1193bccd257fb8a",
	"03e647c014f55265da506781f0b2d67674c35cb59b873d9926d483c4ced9a7bbd3"}

// SetTreasuryPolicy saves the voting policy for treasury spends by a particular
// PI key.
// If a ticket hash is provided, the voting policy is also updated with the VSP
// controlling the ticket. If a ticket hash isn't provided, the vote choice is
// saved to the local wallet database and the VSPs controlling all unspent,
// unexpired tickets are updated to use the specified vote policy.
func (wallet *Wallet) SetTreasuryPolicy(PiKey, newVotingPolicy, tixHash string, passphrase []byte) error {
	var ticketHash *chainhash.Hash
	if tixHash != "" {
		tixHash, err := chainhash.NewHashFromStr(tixHash)
		if err != nil {
			return fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = tixHash
	}

	// The wallet will need to be unlocked to sign the API
	// request(s) for setting this voting policy with the VSP.
	err := wallet.UnlockWallet(passphrase)
	if err != nil {
		return translateError(err)
	}
	defer wallet.LockWallet()

	ctx := wallet.shutdownContext()

	pikey, err := hex.DecodeString(PiKey)
	if err != nil {
		return fmt.Errorf("parameter contains invalid hexadecimal: %w", err)
	}
	if len(pikey) != secp256k1.PubKeyBytesLenCompressed {
		err := errors.New("treasury key must be 33 bytes")
		return err
	}

	currentVotingPolicy := wallet.Internal().TreasuryKeyPolicy(pikey, ticketHash)

	var policy stake.TreasuryVoteT
	switch newVotingPolicy {
	case "abstain", "invalid", "":
		policy = stake.TreasuryVoteInvalid
	case "yes":
		policy = stake.TreasuryVoteYes
	case "no":
		policy = stake.TreasuryVoteNo
	default:
		err := fmt.Errorf("unknown policy %q", newVotingPolicy)
		return fmt.Errorf("invalid policy: %w", err)
	}

	err = wallet.Internal().SetTreasuryKeyPolicy(ctx, pikey, policy, ticketHash)
	if err != nil {
		return err
	}

	// Update voting preferences on VSPs if required.
	policyMap := map[string]string{
		PiKey: newVotingPolicy,
	}

	var vspPreferenceUpdateSuccess bool
	defer func() {
		if !vspPreferenceUpdateSuccess {
			// Updating the treaury spend voting preference with the vsp failed,
			// revert the locally saved voting preference for the treasury spend.
			revertError := wallet.Internal().SetTreasuryKeyPolicy(ctx, pikey, currentVotingPolicy, ticketHash)
			if revertError != nil {
				log.Errorf("unable to revert locally saved voting preference: %v", revertError)
			}
		}
	}()

	// If a ticket hash is provided, set the specified vote policy with
	// the VSP associated with the provided ticket. Otherwise, set the
	// vote policy with the VSPs associated with all "votable" tickets.
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

		// Update the vote policy for the ticket with the associated VSP.
		vspClient, err := wallet.VSPClient(vspTicketInfo.Host, vspTicketInfo.PubKey)
		if err != nil && firstErr == nil {
			firstErr = err
			continue // try next tHash
		}
		err = vspClient.SetVoteChoice(ctx, tHash, nil, nil, policyMap)
		if err != nil && firstErr == nil {
			firstErr = err
			continue // try next tHash
		}
	}

	vspPreferenceUpdateSuccess = firstErr == nil
	return firstErr
}

// TreasuryPolicies returns saved voting policies for treasury spends per pi key.
// If a pi key is specified, the policy for that pi key is returned; otherwise the policies
// for all pi keys are returned. If a ticket hash is provided, the policy(ies) for that ticket
// is/are returned.
func (wallet *Wallet) TreasuryPolicies(PiKey, tixHash string) ([]*TreasuryKeyPolicy, error) {
	var ticketHash *chainhash.Hash
	if tixHash != "" {
		tixHash, err := chainhash.NewHashFromStr(tixHash)
		if err != nil {
			return nil, fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = tixHash
	}

	if PiKey != "" {
		pikey, err := hex.DecodeString(PiKey)
		if err != nil {
			return nil, fmt.Errorf("parameter contains invalid hexadecimal: %w", err)
		}
		var policy string
		switch wallet.Internal().TreasuryKeyPolicy(pikey, ticketHash) {
		case stake.TreasuryVoteYes:
			policy = "yes"
		case stake.TreasuryVoteNo:
			policy = "no"
		default:
			policy = "abstain"
		}
		res := []*TreasuryKeyPolicy{
			{
				PiKey:  PiKey,
				Policy: policy,
			},
		}
		if tixHash != "" {
			res[0].TicketHash = tixHash
		}
		return res, nil
	}

	policies := wallet.Internal().TreasuryKeyPolicies()
	res := make([]*TreasuryKeyPolicy, 0, len(policies))
	for i := range policies {
		var policy string
		switch policies[i].Policy {
		case stake.TreasuryVoteYes:
			policy = "yes"
		case stake.TreasuryVoteNo:
			policy = "no"
		}
		r := &TreasuryKeyPolicy{
			PiKey:  hex.EncodeToString(policies[i].PiKey),
			Policy: policy,
		}
		if policies[i].Ticket != nil {
			r.TicketHash = policies[i].Ticket.String()
		}
		res = append(res, r)
	}
	return res, nil
}
