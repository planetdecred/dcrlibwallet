package dcrlibwallet

// AllWallets returns an array of loaded wallets.
func (mw *MultiWallet) AllWallets() (wallets []*Wallet) {
	for _, wallet := range mw.wallets {
		wallets = append(wallets, wallet)
	}
	return wallets
}

// WalletsIterator returns an iterator for mw wallets.
func (mw *MultiWallet) WalletsIterator() *WalletsIterator {
	return &WalletsIterator{
		currentIndex: 0,
		wallets:      mw.AllWallets(),
	}
}

// Next iterates returns the wallet at the current iterator
// index and increments the iterator index.
func (walletsIterator *WalletsIterator) Next() *Wallet {
	if walletsIterator.currentIndex < len(walletsIterator.wallets) {
		wallet := walletsIterator.wallets[walletsIterator.currentIndex]
		walletsIterator.currentIndex++
		return wallet
	}

	return nil
}

// Reset sets the iterator index to 0.
func (walletsIterator *WalletsIterator) Reset() {
	walletsIterator.currentIndex = 0
}
