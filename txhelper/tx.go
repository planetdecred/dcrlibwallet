package txhelper

import (
	"encoding/binary"
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet"
)

const BlockValid int = 1 << 0

func DecodeTransaction(hash *chainhash.Hash, serializedTx []byte, netParams *chaincfg.Params, addressInfoFn func(string) (*AddressInfo, interface{})) (tx *DecodedTransaction, err error) {
	msgTx, txFee, txSize, txFeeRate, err := MsgTxFeeSizeRate(serializedTx)
	if err != nil {
		return
	}

	var ssGenVersion uint32
	var lastBlockValid bool
	var votebits string
	if stake.IsSSGen(msgTx) {
		ssGenVersion = voteVersion(msgTx)
		lastBlockValid = voteBits(msgTx)&uint16(BlockValid) != 0
		votebits = fmt.Sprintf("%#04x", voteBits(msgTx))
	}

	// wrapper for main address info function to absolve errors and always return an address info
	getAddressInfo := func(address string) *AddressInfo {
		addressInfo, _ := addressInfoFn(address)
		if addressInfo == nil {
			addressInfo = &AddressInfo{Address:address}
		}
		return addressInfo
	}

	tx = &DecodedTransaction{
		Hash:           hash.String(),
		Type:           TransactionType(wallet.TxTransactionType(msgTx)),
		Version:        int32(msgTx.Version),
		LockTime:       int32(msgTx.LockTime),
		Expiry:         int32(msgTx.Expiry),
		Size:           txSize,
		FeeRate:        int64(txFeeRate),
		Fee:            int64(txFee),
		Inputs:         decodeTxInputs(msgTx),
		Outputs:        decodeTxOutputs(msgTx, netParams, getAddressInfo),
		VoteVersion:    int32(ssGenVersion),
		LastBlockValid: lastBlockValid,
		VoteBits:       votebits,
	}
	return
}

func decodeTxInputs(mtx *wire.MsgTx) []*DecodedInput {
	inputs := make([]*DecodedInput, len(mtx.TxIn))
	for i, txIn := range mtx.TxIn {
		inputs[i] = &DecodedInput{
			PreviousTransactionHash:  txIn.PreviousOutPoint.Hash.String(),
			PreviousTransactionIndex: int32(txIn.PreviousOutPoint.Index),
			AmountIn:                 txIn.ValueIn,
			PreviousOutpoint:         txIn.PreviousOutPoint.String(),
		}
	}
	return inputs
}

func decodeTxOutputs(mtx *wire.MsgTx, chainParams *chaincfg.Params, getAddressInfo func(string) *AddressInfo) []*DecodedOutput {
	outputs := make([]*DecodedOutput, len(mtx.TxOut))
	txType := stake.DetermineTxType(mtx)

	for i, v := range mtx.TxOut {
		var addrs []dcrutil.Address
		var addresses []*AddressInfo
		var scriptClass txscript.ScriptClass

		if (txType == stake.TxTypeSStx) && (stake.IsStakeSubmissionTxOut(i)) {
			scriptClass = txscript.StakeSubmissionTy
			addr, err := stake.AddrFromSStxPkScrCommitment(v.PkScript, chainParams)
			if err != nil {
				addresses = []*AddressInfo{{
					Address: fmt.Sprintf("[error] failed to decode ticket commitment addr output for tx hash %v, output idx %v",
						mtx.TxHash(), i),
				}}
			} else {
				addresses = []*AddressInfo{
					getAddressInfo(addr.EncodeAddress()),
				}
			}
		} else {
			// Ignore the error here since an error means the script
			// couldn't parse and there is no additional information
			// about it anyways.
			scriptClass, addrs, _, _ = txscript.ExtractPkScriptAddrs(v.Version, v.PkScript, chainParams)
			addresses = make([]*AddressInfo, len(addrs))
			for j, addr := range addrs {
				addresses[j] = getAddressInfo(addr.EncodeAddress())
			}
		}

		outputs[i] = &DecodedOutput{
			Index:      int32(i),
			Value:      v.Value,
			Version:    int32(v.Version),
			Addresses:  addresses,
			ScriptType: scriptClass.String(),
		}
	}

	return outputs
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
