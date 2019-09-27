package txhelper

import (
	"fmt"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/raedahgroup/dcrlibwallet/addresshelper"
)

func MakeTxOutput(address string, amountInAtom int64) (output *wire.TxOut, err error) {
	pkScript, err := addresshelper.PkScript(address)
	if err != nil {
		return
	}

	output = &wire.TxOut{
		Value:    amountInAtom,
		Version:  txscript.DefaultScriptVersion,
		PkScript: pkScript,
	}
	return
}

// ParseOutputsAndChangeDestination generates and returns TxOuts
// using the provided slice of transaction destinations.
// Any destination set to receive max amount is not included in the TxOuts returned,
// but is instead returned as a change destination.
// Returns an error if more than 1 max amount recipients identified or
// if any other error is encountered while processing the addresses and amounts.
func ParseOutputsAndChangeDestination(txDestinations []TransactionDestination) ([]*wire.TxOut, int64, string, error) {
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

		output, err := MakeTxOutput(destination.Address, destination.AtomAmount)
		if err != nil {
			return nil, 0, "", fmt.Errorf("make tx output error: %v", err)
		}

		totalSendAmount += output.Value
		outputs = append(outputs, output)
	}

	return outputs, totalSendAmount, maxAmountRecipientAddress, nil
}
