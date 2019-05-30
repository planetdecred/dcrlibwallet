package txhelper

import (
	"fmt"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/txrules"
)

func EstimateChange(numberOfInputs int, totalInputAmount int64, destinations []TransactionDestination, changeAddresses []string) (int64, error) {
	// check if there's a max amount recipient, such recipient ideally belongs in the changeAddresses slice
	for _, destination := range destinations {
		if destination.SendMax {
			return 0, fmt.Errorf("this tx will produce no change because one or more recipients are set to receive max amount")
		}
	}

	outputs, totalSendAmount, err := makeTxOutputs(destinations)
	if err != nil {
		return 0, err
	}

	changeAmount, err := EstimateChangeWithOutputs(numberOfInputs, totalInputAmount, outputs, totalSendAmount, changeAddresses)
	if err != nil {
		return 0, err
	}

	if changeAmount < 0 {
		excessSpending := 0 - changeAmount // equivalent to math.Abs()
		return 0, fmt.Errorf("total send amount plus tx fee is higher than the total input amount by %s",
			dcrutil.Amount(excessSpending).String())
	}

	return changeAmount, nil
}

func EstimateMaxSendAmount(numberOfInputs int, totalInputAmount int64, destinations []TransactionDestination) (int64, error) {
	// check if there's a max amount recipient, and not more than 1 such recipient
	var maxAmountRecipientAddress string
	for _, destination := range destinations {
		if destination.SendMax && maxAmountRecipientAddress != "" {
			return 0, fmt.Errorf("cannot send max amount to multiple recipients")
		} else if destination.SendMax {
			maxAmountRecipientAddress = destination.Address
		}
	}

	if maxAmountRecipientAddress == "" {
		return 0, fmt.Errorf("specify the destination address to send max amount to")
	}

	// create transaction outputs for all destination addresses and amounts, excluding destination for send max
	var totalSendAmount int64
	outputs := make([]*wire.TxOut, 0, len(destinations)-1)
	for _, destination := range destinations {
		if !destination.SendMax {
			output, err := MakeTxOutput(destination)
			if err != nil {
				return 0, err
			}

			outputs = append(outputs, output)
			totalSendAmount += output.Value
		}
	}

	// use max recipient address as change address to get max amount
	changeAmount, err := EstimateChangeWithOutputs(numberOfInputs, totalInputAmount, outputs, totalSendAmount, []string{maxAmountRecipientAddress})
	if err != nil {
		return 0, err
	}

	if changeAmount < 0 {
		excessSpending := 0 - changeAmount // equivalent to math.Abs()
		return 0, fmt.Errorf("total send amount plus tx fee will be higher than the total input amount by %s",
			dcrutil.Amount(excessSpending).String())
	}

	return changeAmount, nil
}

func EstimateTxFeeAndSize(numberOfInputs int, sendDestinations, changeDestinations []TransactionDestination) (
	fee dcrutil.Amount, size int, err error) {

	// separate valid tx recipients (outputs) from max amount recipient
	outputs, _, maxAmountRecipientAddress, err := TxOutputsExtractMaxDestinationAddress(sendDestinations)
	if err != nil {
		err = fmt.Errorf("error processing tx recipients: %s", err.Error())
		return
	}

	var changeAddresses []string
	if maxAmountRecipientAddress != "" {
		// if a max amount recipient is specified, use it as the only change destination
		changeAddresses = []string{maxAmountRecipientAddress}
	} else {
		// no max amount recipient, use provided change destinations
		for _, changeDestination := range changeDestinations {
			changeAddresses = append(changeAddresses, changeDestination.Address)
		}
	}

	inputsScriptSizes := make([]int, numberOfInputs)
	for i := 0; i < numberOfInputs; i++ {
		inputsScriptSizes[i] = RedeemP2PKHSigScriptSize
	}

	totalChangeScriptSize, err := calculateChangeScriptSize(changeAddresses)
	if err != nil {
		err = fmt.Errorf("error processing change outputs: %s", err.Error())
		return
	}

	size = EstimateSerializeSize(inputsScriptSizes, outputs, totalChangeScriptSize)
	fee = txrules.FeeForSerializeSize(txrules.DefaultRelayFeePerKb, size)

	return
}

func EstimateChangeWithOutputs(numberOfInputs int, totalInputAmount int64, outputs []*wire.TxOut, totalSendAmount int64, changeAddresses []string) (int64, error) {
	if totalSendAmount >= totalInputAmount {
		return 0, fmt.Errorf("total send amount (%s) is higher than or equal to the total input amount (%s)",
			dcrutil.Amount(totalSendAmount).String(), dcrutil.Amount(totalInputAmount).String())
	}

	totalChangeScriptSize, err := calculateChangeScriptSize(changeAddresses)
	if err != nil {
		return 0, err
	}

	scriptSizes := make([]int, numberOfInputs)
	for i := 0; i < numberOfInputs; i++ {
		scriptSizes[i] = RedeemP2PKHSigScriptSize
	}

	relayFeePerKb := txrules.DefaultRelayFeePerKb
	maxSignedSize := EstimateSerializeSize(scriptSizes, outputs, totalChangeScriptSize)
	maxRequiredFee := txrules.FeeForSerializeSize(relayFeePerKb, maxSignedSize)
	changeAmount := totalInputAmount - totalSendAmount - int64(maxRequiredFee)

	// if change amount is valid, check if the script size exceeds maximum script size
	if changeAmount > 0 && !txrules.IsDustAmount(dcrutil.Amount(changeAmount), totalChangeScriptSize, relayFeePerKb) {
		maxChangeScriptSize := len(changeAddresses) * txscript.MaxScriptElementSize
		if totalChangeScriptSize > maxChangeScriptSize {
			return 0, errors.New("script size exceed maximum bytes pushable to the stack")
		}
	}

	return changeAmount, nil
}
