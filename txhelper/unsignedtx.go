package txhelper

import (
	"errors"
	"fmt"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet/txrules"
)

func NewUnsignedTx(inputs []*wire.TxIn, destinations []TransactionDestination, changeDestinations []TransactionDestination) (*wire.MsgTx, error) {
	outputs, totalSendAmount, err := makeTxOutputs(destinations)
	if err != nil {
		return nil, err
	}

	var totalInputAmount int64
	scriptSizes := make([]int, 0, len(inputs))
	for _, txIn := range inputs {
		totalInputAmount += txIn.ValueIn
		scriptSizes = append(scriptSizes, RedeemP2PKHSigScriptSize)
	}

	if totalInputAmount < totalSendAmount {
		return nil, errors.New("total amount from selected outputs not enough to cover transaction")
	}

	totalChangeScriptSize, err := calculateChangeScriptSize(changeDestinations)
	if err != nil {
		return nil, err
	}

	maxSignedSize := EstimateSerializeSize(scriptSizes, outputs, totalChangeScriptSize)
	maxRequiredFee := txrules.FeeForSerializeSize(txrules.DefaultRelayFeePerKb, maxSignedSize)
	changeAmount := totalInputAmount - totalSendAmount - int64(maxRequiredFee)

	if changeAmount < 0 {
		return nil, errors.New("total amount from selected outputs not enough to cover transaction fee")
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
			return nil, errors.New("total amount assigned to specified change addresses is higher than actual change amount for transaction")
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

func calculateChangeScriptSize(changeDestinations []TransactionDestination) (int, error) {
	var totalChangeScriptSize int
	for _, changeDestination := range changeDestinations {
		changeSource, err := MakeTxChangeSource(changeDestination.Address)
		if err != nil {
			return 0, err
		}
		totalChangeScriptSize += changeSource.ScriptSize()
	}
	return totalChangeScriptSize, nil
}

func EstimateChange(numberOfInputs int, totalInputAmount int64, destinations []TransactionDestination, changeAddresses []string) (int64, error) {
	outputs, totalSendAmount, err := makeTxOutputs(destinations)
	if err != nil {
		return 0, err
	}

	if totalInputAmount < totalSendAmount {
		return 0, errors.New("total input amount not enough to cover transaction")
	}

	var totalChangeScriptSize int
	for _, changeAddress := range changeAddresses {
		changeSource, err := MakeTxChangeSource(changeAddress)
		if err != nil {
			return 0, err
		}
		totalChangeScriptSize += changeSource.ScriptSize()
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
		return 0, errors.New("total input amount not enough to cover transaction fee")
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
