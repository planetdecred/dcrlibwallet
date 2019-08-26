package dcrlibwallet

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/walletseed"
	"github.com/raedahgroup/dcrlibwallet/txindex"
)

// WalletExists checks if a wallet exists
func (lw *LibWallet) WalletExists() (bool, error) {
	return lw.walletLoader.WalletExists()
}

// CreateWallet creates a new wallet using the passed in passphrase as
// the wallet's privatePassPhrase and a default passphrase as the public passphrase
func (lw *LibWallet) CreateWallet(passphrase string, seedMnemonic string) error {
	log.Info("Creating Wallet")
	if len(seedMnemonic) == 0 {
		return errors.New(ErrEmptySeed)
	}
	pubPass := []byte(wallet.InsecurePubPassphrase)
	privPass := []byte(passphrase)
	seed, err := walletseed.DecodeUserInput(seedMnemonic)
	if err != nil {
		log.Error(err)
		return err
	}

	w, err := lw.walletLoader.CreateNewWallet(pubPass, privPass, seed)
	if err != nil {
		log.Error(err)
		return err
	}
	lw.wallet = w

	log.Info("Created Wallet")
	return nil
}

// OPenWallet opens an existing wallet from a wallet's loader
//database path and public passPhrase
func (lw *LibWallet) OpenWallet(pubPass []byte) error {
	w, err := lw.walletLoader.OpenExistingWallet(pubPass)
	if err != nil {
		log.Error(err)
		return translateError(err)
	}
	lw.wallet = w

	// set database for indexing transactions for faster loading
	// important to do it at this point before wallet operations
	// such as sync and transaction notification are triggered
	// because those operations will need to access the tx index db.
	txIndexDbPath := filepath.Join(lw.walletDataDir, txindex.DbName)
	generateWalletAddress := func() (string, error) {
		return lw.NextAddress(0) // use default account
	}
	addressMatchesWallet := func(address string) (bool, error) {
		return lw.HaveAddress(address), nil
	}

	txIndexDB, err := txindex.Initialize(txIndexDbPath, generateWalletAddress, addressMatchesWallet)
	if err != nil {
		log.Error("error initializing tx index database: %v", err)
		return fmt.Errorf("tx index db initialization failed: %s", err.Error())
	}
	lw.txIndexDB = txIndexDB

	// start tx notification listener now,
	// so we can index txs as the wallet is notified of new/updated txs
	lw.ListenForTxNotification()

	return nil
}

// WalletOPened checks if a wallet is opened
func (lw *LibWallet) WalletOpened() bool {
	return lw.wallet != nil
}

// UnlockWallet unlocks a wallet using a wallets's private passPhrase
func (lw *LibWallet) UnlockWallet(privPass []byte) error {
	loadedWallet, ok := lw.walletLoader.LoadedWallet()
	if !ok {
		return fmt.Errorf("wallet has not been loaded")
	}

	defer func() {
		for i := range privPass {
			privPass[i] = 0
		}
	}()

	err := loadedWallet.Unlock(privPass, nil)
	return err
}

// LockWallet locks a wallet
func (lw *LibWallet) LockWallet() {
	if lw.wallet.Locked() {
		lw.wallet.Lock()
	}
}

func (lw *LibWallet) ChangePrivatePassphrase(oldPass []byte, newPass []byte) error {
	defer func() {
		for i := range oldPass {
			oldPass[i] = 0
		}

		for i := range newPass {
			newPass[i] = 0
		}
	}()

	err := lw.wallet.ChangePrivatePassphrase(oldPass, newPass)
	if err != nil {
		return translateError(err)
	}
	return nil
}

func (lw *LibWallet) ChangePublicPassphrase(oldPass []byte, newPass []byte) error {
	defer func() {
		for i := range oldPass {
			oldPass[i] = 0
		}

		for i := range newPass {
			newPass[i] = 0
		}
	}()

	if len(oldPass) == 0 {
		oldPass = []byte(wallet.InsecurePubPassphrase)
	}
	if len(newPass) == 0 {
		newPass = []byte(wallet.InsecurePubPassphrase)
	}

	err := lw.wallet.ChangePublicPassphrase(oldPass, newPass)
	if err != nil {
		return translateError(err)
	}
	return nil
}

func (lw *LibWallet) CloseWallet() error {
	err := lw.walletLoader.UnloadWallet()
	return err
}

func (lw *LibWallet) DeleteWallet(privatePassphrase []byte) error {
	defer func() {
		for i := range privatePassphrase {
			privatePassphrase[i] = 0
		}
	}()

	wallet, loaded := lw.walletLoader.LoadedWallet()
	if !loaded {
		return errors.New(ErrWalletNotLoaded)
	}

	err := wallet.Unlock(privatePassphrase, nil)
	if err != nil {
		return translateError(err)
	}
	wallet.Lock()

	lw.Shutdown(true)

	log.Info("Deleting Wallet")
	return os.RemoveAll(lw.walletDataDir)
}
