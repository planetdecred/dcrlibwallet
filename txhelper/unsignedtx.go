package txhelper

import (
	"fmt"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/raedahgroup/dcrlibwallet/txhelper/internal/txsizes"
)

type GenerateAddressFunc func() (address string, err error)

// NewUnsignedTx uses the inputs to prepare a tx with outputs for the provided send destinations and change destinations.
// If any of the send destinations is set to receive max amount, that destination address is used as single change destination.
// If no change destinations are provided and no recipient is set to receive max amount,
// a single change destination is created for an address gotten by calling `generateAccountAddress()`.
func NewUnsignedTx(inputs []*wire.TxIn, sendDestinations, changeDestinations []TransactionDestination,
	generateAccountAddress GenerateAddressFunc) (*wire.MsgTx, error) {

	var totalInputAmount int64
	scriptSizes := make([]int, 0, len(inputs))
	for _, txIn := range inputs {
		totalInputAmount += txIn.ValueIn
		scriptSizes = append(scriptSizes, txsizes.RedeemP2PKHSigScriptSize)
	}

	outputs, totalSendAmount, maxChangeDestinations, err := TxOutputsExtractMaxChangeDestination(len(inputs), totalInputAmount, sendDestinations)
	if err != nil {
		return nil, err
	}

	if totalSendAmount > totalInputAmount {
		return nil, fmt.Errorf("total send amount (%s) is higher than the total input amount (%s)",
			dcrutil.Amount(totalSendAmount).String(), dcrutil.Amount(totalInputAmount).String())
	}

	// if a max change destination is returned, use it as the only change destination
	if len(maxChangeDestinations) == 1 {
		changeDestinations = maxChangeDestinations
	}

	// create a default change destination if none is specified and there is a change amount from this tx
	if len(changeDestinations) == 0 {
		changeAddress, err := generateAccountAddress()
		if err != nil {
			return nil, fmt.Errorf("error generating change address for tx: %s", err.Error())
		}

		changeAmount, err := EstimateChangeWithOutputs(len(inputs), totalInputAmount, outputs, totalSendAmount, []string{changeAddress})
		if err != nil {
			return nil, fmt.Errorf("error in getting change amount: %s", err.Error())
		}
		if changeAmount > 0 {
			changeDestinations = append(changeDestinations, TransactionDestination{
				Address: changeAddress,
				Amount:  dcrutil.Amount(changeAmount).ToCoin(),
			})
		}
	}

	changeAddresses := make([]string, len(changeDestinations))
	for i, changeDestination := range changeDestinations {
		changeAddresses[i] = changeDestination.Address
	}
	totalChangeScriptSize, err := calculateChangeScriptSize(changeAddresses)
	if err != nil {
		return nil, fmt.Errorf("error processing change outputs: %s", err.Error())
	}

	maxSignedSize := txsizes.EstimateSerializeSize(scriptSizes, outputs, totalChangeScriptSize)
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
			return nil, fmt.Errorf("script size exceed maximum bytes pushable to the stack")
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
