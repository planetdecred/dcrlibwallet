package txhelper

import (
	"encoding/binary"
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet"
)

const BlockValid = 1 << 0

// DecodeTransaction uses the tx hex provided to retrieve detailed information for a transaction.
func DecodeTransaction(walletTx *WalletTx, netParams *chaincfg.Params) (*Transaction, error) {
	msgTx, txFee, txSize, txFeeRate, err := MsgTxFeeSizeRate(walletTx.RawTx)
	if err != nil {
		return nil, err
	}
	txType := wallet.TxTransactionType(msgTx)

	// only use input/output amounts relating to wallet to correctly determine tx direction
	amount, direction := TransactionAmountAndDirection(walletTx.TotalInputAmount, walletTx.TotalOutputAmount, int64(txFee))

	inputs := decodeTxInputs(msgTx, walletTx.Inputs)
	outputs := decodeTxOutputs(msgTx, netParams)

	ssGenVersion, lastBlockValid, voteBits := voteInfo(msgTx)

	return &Transaction{
		Hash:        msgTx.TxHash().String(),
		Hex:         walletTx.RawTx,
		Timestamp:   walletTx.Timestamp,
		BlockHeight: walletTx.BlockHeight,
		Type:        FormatTransactionType(txType),

		Version:  int32(msgTx.Version),
		LockTime: int32(msgTx.LockTime),
		Expiry:   int32(msgTx.Expiry),
		Fee:      int64(txFee),
		FeeRate:  int64(txFeeRate),
		Size:     txSize,

		Direction: direction,
		Amount:    amount,
		Inputs:    inputs,
		Outputs:   outputs,

		VoteVersion:    int32(ssGenVersion),
		LastBlockValid: lastBlockValid,
		VoteBits:       voteBits,
	}, nil
}

func decodeTxInputs(mtx *wire.MsgTx, walletInputs []*WalletInput) (inputs []*TxInput) {
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
	}

	return
}

func decodeTxOutputs(mtx *wire.MsgTx, netParams *chaincfg.Params) (outputs []*TxOutput) {
	outputs = make([]*TxOutput, len(mtx.TxOut))
	txType := stake.DetermineTxType(mtx)

	for i, txOut := range mtx.TxOut {
		output := &TxOutput{
			Index:   int32(i),
			Amount:  txOut.Value,
			Version: int32(txOut.Version),
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
	}

	return
}

func voteInfo(msgTx *wire.MsgTx) (ssGenVersion uint32, lastBlockValid bool, voteBits string) {
	if stake.IsSSGen(msgTx) {
		ssGenVersion = voteVersion(msgTx)
		bits := binary.LittleEndian.Uint16(msgTx.TxOut[1].PkScript[2:4])
		voteBits = fmt.Sprintf("%#04x", bits)
		lastBlockValid = bits&uint16(BlockValid) != 0
	}
	return
}

func voteVersion(mtx *wire.MsgTx) uint32 {
	if len(mtx.TxOut[1].PkScript) < 8 {
		return 0 // Consensus version absent
	}

	return binary.LittleEndian.Uint32(mtx.TxOut[1].PkScript[4:8])
}
