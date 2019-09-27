package txhelper

import (
	"fmt"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/raedahgroup/dcrlibwallet/txhelper/internal/txsizes"
)

type NextAddressFunc func() (address string, err error)

// NewUnsignedTx uses the inputs to prepare a tx with outputs for the provided send destinations and change destinations.
// If any of the send destinations is set to receive max amount,
// that destination address is used as single change destination.
// If no change destinations are provided and no recipient is set to receive max amount,
// a single change destination is created for an address gotten by calling `nextInternalAddress()`.
func NewUnsignedTx(inputs []*wire.TxIn, sendDestinations, changeDestinations []TransactionDestination,
	nextInternalAddress NextAddressFunc) (*wire.MsgTx, int, error) {

	outputs, totalSendAmount, maxAmountRecipientAddress, err := ParseOutputsAndChangeDestination(sendDestinations)
	if err != nil {
		return nil, 0, err
	}

	if maxAmountRecipientAddress != "" && len(changeDestinations) > 0 {
		return nil, 0, errors.E(errors.Invalid, "no change is generated when sending max amount,"+
			" change destinations must not be provided")
	}

	if maxAmountRecipientAddress == "" && len(changeDestinations) == 0 {
		// no change specified, generate new internal address to use as change (max amount recipient)
		maxAmountRecipientAddress, err = nextInternalAddress()
		if err != nil {
			return nil, 0, fmt.Errorf("error generating internal address to use as change: %s", err.Error())
		}
	}

	var totalInputAmount int64
	inputScriptSizes := make([]int, len(inputs))
	inputScripts := make([][]byte, len(inputs))
	for i, input := range inputs {
		totalInputAmount += input.ValueIn
		inputScriptSizes[i] = txsizes.RedeemP2PKHSigScriptSize
		inputScripts[i] = input.SignatureScript
	}

	var changeScriptSize int
	if maxAmountRecipientAddress != "" {
		changeScriptSize, err = calculateChangeScriptSize(maxAmountRecipientAddress)
	} else {
		changeScriptSize, err = calculateMultipleChangeScriptSize(changeDestinations)
	}
	if err != nil {
		return nil, 0, err
	}

	maxSignedSize := txsizes.EstimateSerializeSize(inputScriptSizes, outputs, changeScriptSize)
	maxRequiredFee := txrules.FeeForSerializeSize(txrules.DefaultRelayFeePerKb, maxSignedSize)
	changeAmount := totalInputAmount - totalSendAmount - int64(maxRequiredFee)

	if changeAmount < 0 {
		excessSpending := 0 - changeAmount // equivalent to math.Abs()
		return nil, 0, fmt.Errorf("total send amount plus tx fee is higher than the total input amount by %s",
			dcrutil.Amount(excessSpending).String())
	}

	if changeAmount != 0 && !txrules.IsDustAmount(dcrutil.Amount(changeAmount), changeScriptSize, txrules.DefaultRelayFeePerKb) {
		if changeScriptSize > txscript.MaxScriptElementSize {
			return nil, 0, fmt.Errorf("script size exceed maximum bytes pushable to the stack")
		}

		if maxAmountRecipientAddress != "" {
			singleChangeDestination := TransactionDestination{
				Address:    maxAmountRecipientAddress,
				AtomAmount: changeAmount,
			}
			changeDestinations = []TransactionDestination{singleChangeDestination}
		}

		var totalChangeAmount int64
		for _, changeDestination := range changeDestinations {
			changeOutput, err := MakeTxOutput(changeDestination.Address, changeDestination.AtomAmount)
			if err != nil {
				return nil, 0, fmt.Errorf("change address error: %v", err)
			}

			totalChangeAmount += changeOutput.Value
			outputs = append(outputs, changeOutput)

			// randomize the change output that was just added
			changeOutputIndex := len(outputs) - 1
			txauthor.RandomizeOutputPosition(outputs, changeOutputIndex)
		}

		if totalChangeAmount > changeAmount {
			return nil, 0, fmt.Errorf("total amount allocated to change addresses (%s) is higher than"+
				" actual change amount for transaction (%s)", dcrutil.Amount(totalChangeAmount).String(),
				dcrutil.Amount(changeAmount).String())
		}
	} else {
		maxSignedSize = txsizes.EstimateSerializeSize(inputScriptSizes, outputs, 0)
	}

	return &wire.MsgTx{
		SerType:  wire.TxSerializeFull,
		Version:  wire.TxVersion,
		TxIn:     inputs,
		TxOut:    outputs,
		LockTime: 0,
		Expiry:   0,
	}, maxSignedSize, nil
}

func calculateChangeScriptSize(changeAddress string) (int, error) {
	changeSource, err := MakeTxChangeSource(changeAddress)
	if err != nil {
		return 0, fmt.Errorf("change address error: %v", err)
	}
	return changeSource.ScriptSize(), nil
}

func calculateMultipleChangeScriptSize(changeDestinations []TransactionDestination) (int, error) {
	var totalChangeScriptSize int
	for _, changeDestination := range changeDestinations {
		changeScriptSize, err := calculateChangeScriptSize(changeDestination.Address)
		if err != nil {
			return 0, err
		}
		totalChangeScriptSize += changeScriptSize
	}
	return totalChangeScriptSize, nil
}
