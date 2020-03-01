package txhelper

import (
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/raedahgroup/dcrlibwallet/addresshelper"
)

// implements Script() and ScriptSize() functions of txauthor.ChangeSource
type TxChangeSource struct {
	SrcAccount int32
	version    uint16
	script     []byte
}

func (src *TxChangeSource) Script() ([]byte, uint16, error) {
	return src.script, src.version, nil
}

func (src *TxChangeSource) ScriptSize() int {
	return len(src.script)
}

func MakeTxChangeSource(srcAccount int32, destAddr string, net dcrutil.AddressParams) (*TxChangeSource, error) {
	pkScript, err := addresshelper.PkScript(destAddr, net)
	if err != nil {
		return nil, err
	}
	changeSource := &TxChangeSource{
		SrcAccount: srcAccount,
		script:     pkScript,
		version:    addresshelper.DefaultScriptVersion,
	}
	return changeSource, nil
}
