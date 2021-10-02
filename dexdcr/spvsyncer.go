package dexdcr

import (
	"fmt"

	dcrwalletspv "decred.org/dcrwallet/v2/spv"
	dcrlibwalletspv "github.com/planetdecred/dcrlibwallet/spv"
)

// SpvSyncer defines methods we expect to find in an spv wallet backend.
type SpvSyncer interface {
	Synced() bool
	EstimateMainChainTip() int32
}

// syncer returns the spv syncer connected to the wallet or returns an error
// if the wallet isn't connected to an spv syncer backend. Currently, only the
// spv backends provided by dcrwallet and dcrlibwallet are supported.
func (w *SpvWallet) spvSyncer() (SpvSyncer, error) {
	n, err := w.Wallet.NetworkBackend()
	if err != nil {
		return nil, fmt.Errorf("wallet network backend error: %w", err)
	}

	switch be := n.(type) {
	case *dcrlibwalletspv.WalletBackend:
		return be.Syncer, nil
	case *dcrwalletspv.Syncer:
		return be, nil
	default:
		return nil, fmt.Errorf("wallet is not connected to a supported spv backend")
	}
}
