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
			return nil, fmt.Errorf("error creating change outputs for tx: %s", err.Error())
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
