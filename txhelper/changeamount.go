package txhelper

import (
	"errors"
	"fmt"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet/txrules"
)

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
