package txhelper

import (
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/raedahgroup/mobilewallet/address"
)

// from "github.com/decred/dcrwallet/wallet/internal/txsizes"
// RedeemP2PKHSigScriptSize is the worst case (largest) serialize size
// of a transaction input script that redeems a compressed P2PKH output.
// It is calculated as:
//
//   - OP_DATA_73
//   - 72 bytes DER signature + 1 byte sighash
//   - OP_DATA_33
//   - 33 bytes serialized compressed pubkey
const RedeemP2PKHSigScriptSize = 1 + 73 + 1 + 33

// from "github.com/decred/dcrwallet/wallet/internal/txsizes"
// EstimateSerializeSize returns a worst case serialize size estimate for a
// signed transaction that spends a number of outputs and contains each
// transaction output from txOuts. The estimated size is incremented for an
// additional change output if changeScriptSize is greater than 0. Passing 0
// does not add a change output.
func EstimateSerializeSize(scriptSizes []int, txOuts []*wire.TxOut, changeScriptSize int) int {
	// Generate and sum up the estimated sizes of the inputs.
	txInsSize := 0
	for _, size := range scriptSizes {
		txInsSize += EstimateInputSize(size)
	}

	inputCount := len(scriptSizes)
	outputCount := len(txOuts)
	changeSize := 0
	if changeScriptSize > 0 {
		changeSize = EstimateOutputSize(changeScriptSize)
		outputCount++
	}

	// 12 additional bytes are for version, locktime and expiry.
	return 12 + (2 * wire.VarIntSerializeSize(uint64(inputCount))) +
		wire.VarIntSerializeSize(uint64(outputCount)) +
		txInsSize +
		SumOutputSerializeSizes(txOuts) +
		changeSize
}

// from "github.com/decred/dcrwallet/wallet/internal/txsizes"
// EstimateInputSize returns the worst case serialize size estimate for a tx input
//   - 32 bytes previous tx
//   - 4 bytes output index
//   - 1 byte tree
//   - 8 bytes amount
//   - 4 bytes block height
//   - 4 bytes block index
//   - the compact int representation of the script size
//   - the supplied script size
//   - 4 bytes sequence
func EstimateInputSize(scriptSize int) int {
	return 32 + 4 + 1 + 8 + 4 + 4 + wire.VarIntSerializeSize(uint64(scriptSize)) + scriptSize + 4
}

// from "github.com/decred/dcrwallet/wallet/internal/txsizes"
// EstimateOutputSize returns the worst case serialize size estimate for a tx output
//   - 8 bytes amount
//   - 2 bytes version
//   - the compact int representation of the script size
//   - the supplied script size
func EstimateOutputSize(scriptSize int) int {
	return 8 + 2 + wire.VarIntSerializeSize(uint64(scriptSize)) + scriptSize
}

// from github.com/decred/dcrwallet/internal/helpers
// SumOutputSerializeSizes sums up the serialized size of the supplied outputs.
func SumOutputSerializeSizes(outputs []*wire.TxOut) (serializeSize int) {
	for _, txOut := range outputs {
		serializeSize += txOut.SerializeSize()
	}
	return serializeSize
}

// implements Script() and ScriptSize() functions of txauthor.ChangeSource
type txChangeSource struct {
	version uint16
	script  []byte
}

func (src *txChangeSource) Script() ([]byte, uint16, error) {
	return src.script, src.version, nil
}

func (src *txChangeSource) ScriptSize() int {
	return len(src.script)
}

func MakeTxChangeSource(destAddr string) (*txChangeSource, error) {
	pkScript, err := address.PkScript(destAddr)
	if err != nil {
		return nil, err
	}
	changeSource := &txChangeSource{
		script:  pkScript,
		version: txscript.DefaultScriptVersion,
	}
	return changeSource, nil
}
