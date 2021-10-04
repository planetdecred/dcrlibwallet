package addresshelper

import (
	"fmt"

	chaincfg "github.com/decred/dcrd/chaincfg/v3"
	dcrutil "github.com/decred/dcrd/dcrutil/v4"
	txscript "github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
)

const scriptVersion = 0

func PkScript(address string, net dcrutil.AddressParams) ([]byte, error) {
	addr, err := stdaddr.DecodeAddress(address, net)
	if err != nil {
		return nil, fmt.Errorf("error decoding address '%s': %s", address, err.Error())
	}

	_, pkScript := addr.PaymentScript()
	return pkScript, nil
}

func PkScriptAddresses(params *chaincfg.Params, pkScript []byte) ([]string, error) {
	_, addresses, _, err := txscript.ExtractPkScriptAddrs(scriptVersion, pkScript, params, true)
	if err != nil {
		return nil, err
	}

	encodedAddresses := make([]string, len(addresses))
	for i, address := range addresses {
		encodedAddresses[i] = address.String()
	}

	return encodedAddresses, nil
}
