package dcrlibwallet

import (
	w "github.com/decred/dcrwallet/wallet/v3"
)

// InternalWallet returns the internal dcrwallet.Wallet.
// Only for testing purposes.
func (wallet *Wallet) InternalWallet() *w.Wallet {
	return wallet.internal
}
