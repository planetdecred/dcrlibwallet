package dcrlibwallet

import (
	"fmt"

	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/planetdecred/dcrlibwallet/txhelper"
)

type NextAddressFunc func() (address string, err error)

func (tx *TxAuthor) calculateChangeScriptSize(changeAddress string) (int, error) {
	changeSource, err := txhelper.MakeTxChangeSource(changeAddress, tx.sourceWallet.chainParams)
	if err != nil {
		return 0, fmt.Errorf("change address error: %v", err)
	}
	return changeSource.ScriptSize(), nil
}

func (tx *TxAuthor) calculateMultipleChangeScriptSize(changeDestinations []TransactionDestination) (int, error) {
	var totalChangeScriptSize int
	for _, changeDestination := range changeDestinations {
		changeScriptSize, err := tx.calculateChangeScriptSize(changeDestination.Address)
		if err != nil {
			return 0, err
		}
		totalChangeScriptSize += changeScriptSize
	}
	return totalChangeScriptSize, nil
}

// ParseOutputsAndChangeDestination generates and returns TxOuts
// using the provided slice of transaction destinations.
// Any destination set to receive max amount is not included in the TxOuts returned,
// but is instead returned as a change destination.
// Returns an error if more than 1 max amount recipients identified or
// if any other error is encountered while processing the addresses and amounts.
func (tx *TxAuthor) ParseOutputsAndChangeDestination(txDestinations []TransactionDestination) ([]*wire.TxOut, int64, string, error) {
	var outputs = make([]*wire.TxOut, 0)
	var totalSendAmount int64
	var maxAmountRecipientAddress string

	for _, destination := range txDestinations {
		// validate the amount to send to this destination address
		if !destination.SendMax && (destination.AtomAmount <= 0 || destination.AtomAmount > dcrutil.MaxAmount) {
			return nil, 0, "", errors.E(errors.Invalid, "invalid amount")
		}

		// check if multiple destinations are set to receive max amount
		if destination.SendMax && maxAmountRecipientAddress != "" {
			return nil, 0, "", fmt.Errorf("cannot send max amount to multiple recipients")
		}

		if destination.SendMax {
			maxAmountRecipientAddress = destination.Address
			continue // do not prepare a tx output for this destination
		}

		output, err := txhelper.MakeTxOutput(destination.Address, destination.AtomAmount, tx.sourceWallet.chainParams)
		if err != nil {
			return nil, 0, "", fmt.Errorf("make tx output error: %v", err)
		}

		totalSendAmount += output.Value
		outputs = append(outputs, output)
	}

	return outputs, totalSendAmount, maxAmountRecipientAddress, nil
}
