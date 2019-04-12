package txhelper

import (
	"fmt"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
)

// TxOutputsExtractMaxDestinationAddress checks the provided txDestinations
// if there is 1 and not more than 1 recipient set to receive max amount.
// Returns an error if more than 1 max amount recipients identified.
// Returns the outputs for the tx excluding the max amount recipient,
// and also returns the max amount recipient address if there is one.
func TxOutputsExtractMaxDestinationAddress(txDestinations []TransactionDestination) (
	outputs []*wire.TxOut, totalSendAmount int64, maxAmountRecipientAddress string, err error) {

	// check if there's a max amount recipient, and not more than 1 such recipient
	nOutputs := len(txDestinations)
	for _, destination := range txDestinations {
		if destination.SendMax && maxAmountRecipientAddress != "" {
			err = fmt.Errorf("cannot send max amount to multiple recipients")
			return
		} else if destination.SendMax {
			maxAmountRecipientAddress = destination.Address
			nOutputs--
		}
	}

	// create transaction outputs for all destination addresses and amounts, excluding destination for send max
	outputs = make([]*wire.TxOut, 0, nOutputs)
	var output *wire.TxOut
	for _, destination := range txDestinations {
		if !destination.SendMax {
			output, err = MakeTxOutput(destination)
			if err != nil {
				return
			}

			outputs = append(outputs, output)
			totalSendAmount += output.Value
		}
	}

	return
}

// TxOutputsExtractMaxChangeDestination checks the provided txDestinations
// if there is 1 and not more than 1 recipient set to receive max amount.
// Returns an error if more than 1 max amount recipients identified.
// Returns the outputs for the tx excluding the max amount recipient,
// and also returns the change object for the max amount recipient address if there is a max recipient address.
func TxOutputsExtractMaxChangeDestination(nInputs int, totalInputAmount int64, txDestinations []TransactionDestination) (
	outputs []*wire.TxOut, maxChangeDestinations []TransactionDestination, err error) {

	outputs, totalSendAmount, maxAmountRecipientAddress, err := TxOutputsExtractMaxDestinationAddress(txDestinations)
	if err != nil {
		return
	}

	if maxAmountRecipientAddress != "" {
		// use as change address
		changeAddresses := []string{maxAmountRecipientAddress}
		changeAmount, err := EstimateChangeWithOutputs(nInputs, totalInputAmount, outputs, totalSendAmount, changeAddresses)
		if err != nil {
			return nil, nil, err
		}

		maxChangeDestinations = []TransactionDestination{
			{
				Address: maxAmountRecipientAddress,
				Amount:  dcrutil.Amount(changeAmount).ToCoin(),
			},
		}
	}

	return
}
