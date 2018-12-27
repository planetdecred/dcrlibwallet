package txhelper

import (
	"errors"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/raedahgroup/mobilewallet/address"
)

func NewUnsignedTx(inputs []*wire.TxIn, amount int64, destAddress string, changeAddress string) (*wire.MsgTx, error) {
	outputs, err := makeTxOutputs(amount, destAddress)
	if err != nil {
		return nil, err
	}

	changeSource, err := MakeTxChangeSource(changeAddress)
	if err != nil {
		return nil, err
	}

	changeScript, changeScriptVersion, err := changeSource.Script()
	if err != nil {
		return nil, err
	}
	changeScriptSize := changeSource.ScriptSize()

	var totalInputAmount int64
	scriptSizes := make([]int, 0, len(inputs))
	for _, txIn := range inputs {
		totalInputAmount += txIn.ValueIn
		scriptSizes = append(scriptSizes, RedeemP2PKHSigScriptSize)
	}

	if totalInputAmount < amount {
		return nil, errors.New("total amount from selected outputs not enough to cover transaction")
	}

	relayFeePerKb := txrules.DefaultRelayFeePerKb
	maxSignedSize := EstimateSerializeSize(scriptSizes, outputs, changeScriptSize)
	maxRequiredFee := txrules.FeeForSerializeSize(relayFeePerKb, maxSignedSize)
	changeAmount := totalInputAmount - amount - int64(maxRequiredFee)

	if changeAmount < 0 {
		return nil, errors.New("total amount from selected outputs not enough to cover transaction fee")
	}

	if changeAmount != 0 && !txrules.IsDustAmount(dcrutil.Amount(changeAmount), changeScriptSize, relayFeePerKb) {
		if len(changeScript) > txscript.MaxScriptElementSize {
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

func makeTxOutputs(sendAmount int64, destinationAddress string) ([]*wire.TxOut, error) {
	// get address public script
	pkScript, err := address.PkScript(destinationAddress)
	if err != nil {
		return nil, err
	}

	// create non-change output
	output := wire.NewTxOut(sendAmount, pkScript)
	return []*wire.TxOut{output}, nil
}
