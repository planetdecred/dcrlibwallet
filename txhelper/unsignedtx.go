package txhelper

import (
	"errors"
	"fmt"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet/txrules"
)

func NewUnsignedTx(inputs []*wire.TxIn, outputs []*wire.TxOut, changeDestinations []TransactionDestination) (*wire.MsgTx, error) {
	var totalSendAmount int64
	for _, output := range outputs {
		totalSendAmount += output.Value
	}

	var totalInputAmount int64
	scriptSizes := make([]int, 0, len(inputs))
	for _, txIn := range inputs {
		totalInputAmount += txIn.ValueIn
		scriptSizes = append(scriptSizes, RedeemP2PKHSigScriptSize)
	}

	if totalSendAmount > totalInputAmount {
		return nil, fmt.Errorf("total send amount (%s) is higher than the total input amount (%s)",
			dcrutil.Amount(totalSendAmount).String(), dcrutil.Amount(totalInputAmount).String())
	}

	changeAddresses := make([]string, len(changeDestinations))
	for i, changeDestination := range changeDestinations {
		changeAddresses[i] = changeDestination.Address
	}
	totalChangeScriptSize, err := calculateChangeScriptSize(changeAddresses)
	if err != nil {
		return nil, err
	}

	maxSignedSize := EstimateSerializeSize(scriptSizes, outputs, totalChangeScriptSize)
	maxRequiredFee := txrules.FeeForSerializeSize(txrules.DefaultRelayFeePerKb, maxSignedSize)
	changeAmount := totalInputAmount - totalSendAmount - int64(maxRequiredFee)

	if changeAmount < 0 {
		excessSpending := 0 - changeAmount // equivalent to math.Abs()
		return nil, fmt.Errorf("total send amount plus tx fee is higher than the total input amount by %s",
			dcrutil.Amount(excessSpending).String())
	}

	if changeAmount != 0 && !txrules.IsDustAmount(dcrutil.Amount(changeAmount), totalChangeScriptSize, txrules.DefaultRelayFeePerKb) {
		maxAcceptableChangeScriptSize := len(changeDestinations) * txscript.MaxScriptElementSize
		if totalChangeScriptSize > maxAcceptableChangeScriptSize {
			return nil, errors.New("script size exceed maximum bytes pushable to the stack")
		}

		changeOutputs, totalChangeAmount, err := makeTxOutputs(changeDestinations)
		if err != nil {
			return nil, fmt.Errorf("error creating change outputs: %s", err.Error())
		}

		if totalChangeAmount > changeAmount {
			return nil, fmt.Errorf("total amount allocated to change addresses (%s) is higher than actual change amount for transaction (%s)",
				dcrutil.Amount(totalChangeAmount).String(), dcrutil.Amount(changeAmount).String())
		}

		// todo dcrwallet randomizes change position, should look into that as well
		outputs = append(outputs, changeOutputs...)
	}

	unsignedTransaction := &wire.MsgTx{
		SerType:  wire.TxSerializeFull,
		Version:  wire.TxVersion,
		TxIn:     inputs,
		TxOut:    outputs,
		LockTime: 0,
		Expiry:   0,
	}

	return unsignedTransaction, nil
}

func calculateChangeScriptSize(changeAddresses []string) (int, error) {
	var totalChangeScriptSize int
	for _, changeAddress := range changeAddresses {
		changeSource, err := MakeTxChangeSource(changeAddress)
		if err != nil {
			return 0, err
		}
		totalChangeScriptSize += changeSource.ScriptSize()
	}
	return totalChangeScriptSize, nil
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
		return 0, fmt.Errorf("provide the destination address for which to estimate max send amount")
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
	return EstimateChangeWithOutputs(numberOfInputs, totalInputAmount, outputs, totalSendAmount, []string{maxAmountRecipientAddress})
}

func EstimateChange(numberOfInputs int, totalInputAmount int64, destinations []TransactionDestination, changeAddresses []string) (int64, error) {
	outputs, totalSendAmount, err := makeTxOutputs(destinations)
	if err != nil {
		return 0, err
	}

	return EstimateChangeWithOutputs(numberOfInputs, totalInputAmount, outputs, totalSendAmount, changeAddresses)
}

func EstimateChangeWithOutputs(numberOfInputs int, totalInputAmount int64, outputs []*wire.TxOut, totalSendAmount int64, changeAddresses []string) (int64, error) {
	if totalSendAmount >= totalInputAmount {
		return 0, fmt.Errorf("total send amount (%s) is higher than the total input amount (%s)",
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

	if changeAmount < 0 {
		excessSpending := 0 - changeAmount // equivalent to math.Abs()
		// return negative change amount so that the caller can decide if to use a different error message
		// todo error codes should be used instead
		return changeAmount, fmt.Errorf("total send amount plus tx fee is higher than the total input amount by %s",
			dcrutil.Amount(excessSpending).String())
	}

	if changeAmount != 0 && !txrules.IsDustAmount(dcrutil.Amount(changeAmount), totalChangeScriptSize, relayFeePerKb) {
		maxChangeScriptSize := len(changeAddresses) * txscript.MaxScriptElementSize
		if totalChangeScriptSize > maxChangeScriptSize {
			return 0, errors.New("script size exceed maximum bytes pushable to the stack")
		}
	}

	return changeAmount, nil
}

func makeTxOutputs(destinations []TransactionDestination) (outputs []*wire.TxOut, totalSendAmount int64, err error) {
	for _, destination := range destinations {
		var output *wire.TxOut
		output, err = MakeTxOutput(destination)
		if err != nil {
			return
		}

		outputs = append(outputs, output)
		totalSendAmount += output.Value
	}
	return
}
