package txhelper

import (
	"fmt"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrd/dcrutil"
)

func PrepareTxOutputs(nInputs int, totalInputAmount int64, txDestinations []TransactionDestination) (outputs []*wire.TxOut, maxChangeDestinations []TransactionDestination, err error) {
	// check if there's a max amount recipient, and not more than 1 such recipient
	nOutputs := len(txDestinations)
	var maxAmountRecipientAddress string
	for _, destination := range txDestinations {
		if destination.SendMax && maxAmountRecipientAddress != "" {
			return nil, nil, fmt.Errorf("cannot send max amount to multiple recipients")
		} else {
			maxAmountRecipientAddress = destination.Address
			nOutputs--
		}
	}

	// create transaction outputs for all destination addresses and amounts, excluding destination for send max
	outputs = make([]*wire.TxOut, nOutputs)
	var totalSendAmount int64
	for i, destination := range txDestinations {
		if !destination.SendMax {
			output, err := MakeTxOutput(destination)
			if err != nil {
				return nil, nil, err
			}

			outputs[i] = output
			totalSendAmount += output.Value
		}
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
