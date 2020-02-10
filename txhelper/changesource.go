package txhelper

import (
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/raedahgroup/dcrlibwallet/addresshelper"
)

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

func MakeTxChangeSource(destAddr string, net dcrutil.AddressParams) (*txChangeSource, error) {
	pkScript, err := addresshelper.PkScript(destAddr, net)
	if err != nil {
		return nil, err
	}
	changeSource := &txChangeSource{
		script:  pkScript,
		version: addresshelper.DefaultScriptVersion,
	}
	return changeSource, nil
}
