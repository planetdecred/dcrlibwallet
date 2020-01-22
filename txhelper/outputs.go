package txhelper

import (
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	"github.com/raedahgroup/dcrlibwallet/addresshelper"
)

func MakeTxOutputs(destinations []TransactionDestination, net dcrutil.AddressParams) (outputs []*wire.TxOut, err error) {
	for _, destination := range destinations {
		var output *wire.TxOut
		output, err = MakeTxOutput(destination, net)
		if err != nil {
			return
		}

		outputs = append(outputs, output)
	}
	return
}

func MakeTxOutput(destination TransactionDestination, net dcrutil.AddressParams) (output *wire.TxOut, err error) {
	pkScript, err := addresshelper.PkScript(destination.Address, net)
	if err != nil {
		return
	}

	amountInAtom, err := dcrutil.NewAmount(destination.Amount)
	if err != nil {
		return
	}

	output = &wire.TxOut{
		Value:    int64(amountInAtom),
		Version:  addresshelper.ScriptVersion,
		PkScript: pkScript,
	}
	return
}
