package txhelper

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/txhelpers"
	"github.com/decred/dcrwallet/wallet"
)

const BlockValid = 1 << 0

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

// DecodeTransaction uses the tx hex provided to retrieve detailed information for a transaction.
func DecodeTransaction(walletTx *WalletTx, netParams *chaincfg.Params) (*Transaction, error) {
	msgTx, txFee, txSize, txFeeRate, err := MsgTxFeeSizeRate(walletTx.RawTx)
	if err != nil {
		return nil, err
	}
	txType := wallet.TxTransactionType(msgTx)

	inputs, totalInputAmount := decodeTxInputs(msgTx, walletTx.Inputs)
	outputs, totalOutputAmount := decodeTxOutputs(msgTx, netParams)
	amount, direction := TransactionAmountAndDirection(totalInputAmount, totalOutputAmount, int64(txFee))

	var ssGenVersion uint32
	var lastBlockValid bool
	var votebits string
	if stake.IsSSGen(msgTx) {
		ssGenVersion = voteVersion(msgTx)
		lastBlockValid = voteBits(msgTx)&uint16(BlockValid) != 0
		votebits = fmt.Sprintf("%#04x", voteBits(msgTx))
	}

	return &Transaction{
		Hash:           msgTx.TxHash().String(),
		Hex: 			walletTx.RawTx,
		Timestamp:      walletTx.Timestamp,
		BlockHeight:    walletTx.BlockHeight,
		Type:           FormatTransactionType(txType),

		Version:        int32(msgTx.Version),
		LockTime:       int32(msgTx.LockTime),
		Expiry:         int32(msgTx.Expiry),
		Fee:            int64(txFee),
		FeeRate:        int64(txFeeRate),
		Size:           txSize,

		Direction:      direction,
		Amount:         amount,
		Inputs:         inputs,
		Outputs:        outputs,

		VoteVersion:    int32(ssGenVersion),
		LastBlockValid: lastBlockValid,
		VoteBits:       votebits,
	}, nil
}

func decodeTxInputs(mtx *wire.MsgTx, walletInputs []*WalletInput) (inputs []*TxInput, totalInputAmount int64) {
	inputs = make([]*TxInput, len(mtx.TxIn))

	for i, txIn := range mtx.TxIn {
		input := &TxInput{
			PreviousTransactionHash:  txIn.PreviousOutPoint.Hash.String(),
			PreviousTransactionIndex: int32(txIn.PreviousOutPoint.Index),
			PreviousOutpoint:         txIn.PreviousOutPoint.String(),
			AmountIn:                 txIn.ValueIn,
		}

		// check if this is a wallet input
		for _, walletInput := range walletInputs {
			if walletInput.Index == 1 {
				input.WalletInput = walletInput
				break
			}
		}

		inputs[i] = input
		totalInputAmount += txIn.ValueIn
	}

	return
}

func decodeTxOutputs(mtx *wire.MsgTx, netParams *chaincfg.Params) (outputs []*TxOutput, totalOutputAmount int64) {
	outputs = make([]*TxOutput, len(mtx.TxOut))
	txType := stake.DetermineTxType(mtx)

	for i, txOut := range mtx.TxOut {
		output := &TxOutput{
			Index:      int32(i),
			Amount:      txOut.Value,
			Version:    int32(txOut.Version),
		}

		if (txType == stake.TxTypeSStx) && (stake.IsStakeSubmissionTxOut(i)) {
			addr, err := stake.AddrFromSStxPkScrCommitment(txOut.PkScript, netParams)
			if err == nil {
				output.Address = addr.EncodeAddress()
			}
			output.ScriptType = txscript.StakeSubmissionTy.String()
		} else {
			// Ignore the error here since an error means the script
			// couldn't parse and there is no additional information
			// about it anyways.
			scriptClass, addrs, _, _ := txscript.ExtractPkScriptAddrs(txOut.Version, txOut.PkScript, netParams)
			if len(addrs) > 0 {
				output.Address = addrs[0].EncodeAddress()
			}
			output.ScriptType = scriptClass.String()
		}

		outputs[i] = output
		totalOutputAmount += output.Amount
	}

	return
}

func voteVersion(mtx *wire.MsgTx) uint32 {
	if len(mtx.TxOut[1].PkScript) < 8 {
		return 0 // Consensus version absent
	}

	return binary.LittleEndian.Uint32(mtx.TxOut[1].PkScript[4:8])
}

func voteBits(mtx *wire.MsgTx) uint16 {
	return binary.LittleEndian.Uint16(mtx.TxOut[1].PkScript[2:4])
}
