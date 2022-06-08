package dcrlibwallet

import (
	"encoding/hex"
	"fmt"

	"decred.org/dcrwallet/v2/errors"

	"github.com/decred/dcrd/blockchain/stake/v4"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const (
	PiKey = "03f6e7041f1cf51ee10e0a01cd2b0385ce3cd9debaabb2296f7e9dee9329da946c"
	// PiKeys = [][]byte{
	// 	hexDecode("03f6e7041f1cf51ee10e0a01cd2b0385ce3cd9debaabb2296f7e9dee9329da946c"),
	// 	hexDecode("0319a37405cb4d1691971847d7719cfce70857c0f6e97d7c9174a3998cf0ab86dd"),
	// }
)

// SetTreasuryPolicy saves the voting policy for treasury spends by a particular
// key, and optionally, setting the key policy used by a specific ticket.
//
// If a VSP host is configured in the application settings, the voting
// preferences will also be set with the VSP.
func (wallet *Wallet) SetTreasuryPolicy(key, newVotingPolicy, hash string, passphrase []byte) error {
	var ticketHash *chainhash.Hash
	if hash != "" {
		println("len of hash is", len(hash))
		if len(hash) != chainhash.MaxHashStringSize {
			err := fmt.Errorf("invalid ticket hash length, expected %d got %d",
				chainhash.MaxHashStringSize, len(hash))
			return fmt.Errorf("parameter contains invalid hexadecimal: %w", err)
		}
		var err error
		ticketHash, err = chainhash.NewHashFromStr(hash)
		if err != nil {
			return fmt.Errorf("invalid hash: %w", err)
		}
	}

	// The wallet will need to be unlocked to sign the API
	// request(s) for setting this voting policy with the VSP.
	err := wallet.UnlockWallet(passphrase)
	if err != nil {
		return translateError(err)
	}
	defer wallet.LockWallet()

	ctx := wallet.shutdownContext()

	pikey, err := hex.DecodeString(key)
	if err != nil {
		return fmt.Errorf("parameter contains invalid hexadecimal: %w", err)
	}
	if len(pikey) != secp256k1.PubKeyBytesLenCompressed {
		err := errors.New("treasury key must be 33 bytes")
		return fmt.Errorf("Hashes is not evenly divisible by the hash size: %w", err)
	}

	currentVotingPolicy := wallet.Internal().TreasuryKeyPolicy(pikey, nil)

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
		key: newVotingPolicy,
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
		err = vspClient.SetVoteChoice(ctx, tHash, nil, nil, policyMap)
		if err != nil && firstErr == nil {
			firstErr = err
			continue // try next tHash
		}
	}

	vspPreferenceUpdateSuccess = firstErr == nil
	return firstErr
}
