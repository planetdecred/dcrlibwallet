package txhelper

import (
	"errors"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet/txrules"
)

func NewUnsignedTx(inputs []*wire.TxIn, destinations []TransactionDestination, changeAddress string) (*wire.MsgTx, error) {
	outputs, totalSendAmount, err := makeTxOutputs(destinations)
	if err != nil {
		return nil, err
	}

	changeSource, err := MakeTxChangeSource(changeAddress)
	if err != nil {
		return nil, err
	}
	changeScriptSize := changeSource.ScriptSize()

	changeScript, changeScriptVersion, err := changeSource.Script()
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

	relayFeePerKb := txrules.DefaultRelayFeePerKb
	maxSignedSize := EstimateSerializeSize(scriptSizes, outputs, changeScriptSize)
	maxRequiredFee := txrules.FeeForSerializeSize(relayFeePerKb, maxSignedSize)
	changeAmount := totalInputAmount - totalSendAmount - int64(maxRequiredFee)

	if changeAmount < 0 {
		return nil, errors.New("total amount from selected outputs not enough to cover transaction fee")
	}

	if changeAmount != 0 && !txrules.IsDustAmount(dcrutil.Amount(changeAmount), changeScriptSize, relayFeePerKb) {
		if changeScriptSize > txscript.MaxScriptElementSize {
			return nil, errors.New("script size exceed maximum bytes pushable to the stack")
		}
		// todo dcrwallet randomizes change position, should look into that as well
		change := &wire.TxOut{
			Value:    changeAmount,
			Version:  changeScriptVersion,
			PkScript: changeScript,
		}
		outputs = append(outputs, change)
	}

	unsignedTransaction := &wire.MsgTx{
		SerType:  wire.TxSerializeFull,
		Version:  wire.TxVersion, // dcrwallet uses a custom private var txauthor.generatedTxVersion
		TxIn:     inputs,
		TxOut:    outputs,
		LockTime: 0,
		Expiry:   0,
	}

	return unsignedTransaction, nil
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
