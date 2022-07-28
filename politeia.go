package dcrlibwallet

import (
	// "context"
	// "encoding/json"
	// "fmt"
	// "sync"

	// "decred.org/dcrwallet/v2/errors"
	// "github.com/asdine/storm"
	// "github.com/asdine/storm/q"
	"github.com/planetdecred/dcrlibwallet/wallets/dcr"
)

const (
	ProposalCategoryAll int32 = iota + 1
	ProposalCategoryPre
	ProposalCategoryActive
	ProposalCategoryApproved
	ProposalCategoryRejected
	ProposalCategoryAbandoned
)

func newPoliteia(walletRef *dcr.Wallet, host string) (*dcr.Politeia, error) {
	p := &dcr.Politeia{
		WalletRef:             walletRef,
		Host:                  host,
		Client:                nil,
		NotificationListeners: make(map[string]dcr.ProposalNotificationListener),
	}

	return p, nil
}
