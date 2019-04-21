package txhelper

import (
	"math"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/txhelpers"
)

const (
	// standard decred min confirmations is 2, this should be used as default for wallet operations
	DefaultRequiredConfirmations = 2

	ConfirmedStatus = "Confirmed"

	UnconfirmedStatus = "Pending"
)

func TxConfirmations(txBlockHeight, bestBlockHeight int32) int32 {
	var confirmations int32 = -1
	if txBlockHeight >= 0 {
		confirmations = bestBlockHeight - txBlockHeight + 1
	}
	return confirmations
}

func TxStatus(confirmations int32) string {
	if confirmations >= DefaultRequiredConfirmations {
		return ConfirmedStatus
	} else {
		return UnconfirmedStatus
	}
}

func MsgTxFeeSizeRate(transactionHex string) (msgTx *wire.MsgTx, fee dcrutil.Amount, size int, feeRate dcrutil.Amount, err error) {
	msgTx, err = txhelpers.MsgTxFromHex(transactionHex)
	if err != nil {
		return
	}

	size = msgTx.SerializeSize()
	fee, feeRate = txhelpers.TxFeeRate(msgTx)
	return
}

func TransactionAmountAndDirection(inputTotal, outputTotal, fee int64) (amount int64, direction TransactionDirection) {
	amountDifference := outputTotal - inputTotal

	if amountDifference < 0 && float64(fee) == math.Abs(float64(amountDifference)) {
		// transferred internally, the only real amount spent was transaction fee
		direction = TransactionDirectionTransferred
		amount = fee
	} else if amountDifference > 0 {
		// received
		direction = TransactionDirectionReceived
		amount = outputTotal
	} else {
		// sent
		direction = TransactionDirectionSent
		amount = inputTotal - outputTotal - fee
	}

	return
}
