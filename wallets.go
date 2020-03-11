package dcrlibwallet

// AllWallets returns all the available wallets in a MultiWallet instance.
func (mw *MultiWallet) AllWallets() (wallets []*Wallet) {
	for _, wallet := range mw.wallets {
		wallets = append(wallets, wallet)
	}
	return wallets
}

// WalletsIterator returns all the available wallets and
// their index in a MultiWallet instance.
func (mw *MultiWallet) WalletsIterator() *WalletsIterator {
	return &WalletsIterator{
		currentIndex: 0,
		wallets:      mw.AllWallets(),
	}
}

// Next iterates over the the wallets in a MultiWallet
// instance and returns a wallet that has an index
// within the length of the available wallets.
func (walletsIterator *WalletsIterator) Next() *Wallet {
	if walletsIterator.currentIndex < len(walletsIterator.wallets) {
		wallet := walletsIterator.wallets[walletsIterator.currentIndex]
		walletsIterator.currentIndex++
		return wallet
	}

	return nil
}

// Reset sets the wallet to display at index 0.
func (walletsIterator *WalletsIterator) Reset() {
	walletsIterator.currentIndex = 0
}
