package addresshelper

import (
	"fmt"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
)

// PkScript decodes the string encoding of an address
// and creates a new script to pay a transaction output
// to the decoded address
func PkScript(address string) ([]byte, error) {
	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		return nil, fmt.Errorf("error decoding address '%s': %s", address, err.Error())
	}

	return txscript.PayToAddrScript(addr)
}

// DecodeForNetwork decodes the string encoding of an address
// and returns the decoded address if it's associated with the
// passed network
func DecodeForNetwork(address string, params *chaincfg.Params) (dcrutil.Address, error) {
	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		return nil, err
	}
	if !addr.IsForNet(params) {
		return nil, fmt.Errorf("address %s is not intended for use on %s", address, params.Name)
	}
	return addr, nil
}

func PkScriptAddresses(params *chaincfg.Params, pkScript []byte) ([]string, error) {
	_, addresses, _, err := txscript.ExtractPkScriptAddrs(txscript.DefaultScriptVersion, pkScript, params)
	if err != nil {
		return nil, err
	}

	encodedAddresses := make([]string, len(addresses))
	for i, address := range addresses {
		encodedAddresses[i] = address.EncodeAddress()
	}

	return encodedAddresses, nil
}
