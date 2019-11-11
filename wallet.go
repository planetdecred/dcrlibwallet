package dcrlibwallet

import (
	"fmt"
	"os"
	"strings"

	"github.com/asdine/storm"
	"github.com/decred/dcrwallet/errors/v2"
	wallet "github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/walletseed"
)

func (lw *LibWallet) NetType() string {
	return lw.activeNet.Name
}

func (lw *LibWallet) WalletExists() (bool, error) {
	return lw.walletLoader.WalletExists()
}

func (lw *LibWallet) GetName() string {
	return lw.wallet.Name
}

func (lw *LibWallet) GetWalletID() int {
	return lw.wallet.ID
}

func (lw *LibWallet) GetSpendingPassphraseType() int32 {
	return lw.wallet.PrivatePassphraseType
}

func (lw *LibWallet) GetWalletSeed() string {
	return lw.wallet.Seed
}

func (mw *MultiWallet) VerifySeed(walletID int, seedMnemonic string) error {
	lw, ok := mw.libWallets[walletID]
	if !ok {
		return errors.New(ErrNotExist)
	}

	if lw.wallet.Seed == seedMnemonic {
		lw.wallet.Seed = ""
		return translateError(mw.db.Save(lw.wallet))
	}

	return errors.New(ErrInvalid)
}

func (lw *LibWallet) CreateWallet(privatePassphrase string, seedMnemonic string) error {
	log.Info("Creating Wallet")
	if len(seedMnemonic) == 0 {
		return errors.New(ErrEmptySeed)
	}
	pubPass := []byte(wallet.InsecurePubPassphrase)
	privPass := []byte(privatePassphrase)
	seed, err := walletseed.DecodeUserInput(seedMnemonic)
	if err != nil {
		log.Error(err)
		return err
	}

	ctx, _ := lw.contextWithShutdownCancel()
	w, err := lw.walletLoader.CreateNewWallet(ctx, pubPass, privPass, seed)
	if err != nil {
		log.Error(err)
		return err
	}
	lw.wallet.Wallet = w

	log.Info("Created Wallet")
	return nil
}

func (lw *LibWallet) CreateWatchingOnlyWallet(publicPassphrase, extendedPublicKey string) error {

	pubPass := []byte(publicPassphrase)

	ctx, _ := lw.contextWithShutdownCancel()
	w, err := lw.walletLoader.CreateWatchingOnlyWallet(ctx, extendedPublicKey, pubPass)
	if err != nil {
		log.Error(err)
		return err
	}
	lw.wallet.Wallet = w

	log.Info("Created Watching Only Wallet")
	return nil
}

func (lw *LibWallet) IsWatchingOnlyWallet() bool {
	if w, ok := lw.walletLoader.LoadedWallet(); ok {
		return w.Manager.WatchingOnly()
	}

	return false
}

func (lw *LibWallet) OpenWallet(pubPass []byte) error {
	// if lw.ReadBoolConfigValueForKey(IsStartupSecuritySetConfigKey) && pubPass == nil {
	// 	return fmt.Errorf("public passphrase is required")
	// }
	if pubPass == nil {
		pubPass = []byte("public")
	}

	ctx, _ := lw.contextWithShutdownCancel()
	w, err := lw.walletLoader.OpenExistingWallet(ctx, pubPass)
	if err != nil {
		log.Error(err)
		return translateError(err)
	}
	lw.wallet.Wallet = w
	return nil
}

func (lw *LibWallet) WalletOpened() bool {
	return lw.wallet != nil
}

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

	ctx, _ := lw.contextWithShutdownCancel()
	err := loadedWallet.Unlock(ctx, privPass, nil)
	if err != nil {
		return translateError(err)
	}

	return nil
}

func (lw *LibWallet) LockWallet() {
	if !lw.wallet.Locked() {
		lw.wallet.Lock()
	}
}

func (lw *LibWallet) IsLocked() bool {
	return lw.wallet.Locked()
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

	ctx, _ := lw.contextWithShutdownCancel()
	err := lw.wallet.ChangePrivatePassphrase(ctx, oldPass, newPass)
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

	ctx, _ := lw.contextWithShutdownCancel()
	err := lw.wallet.ChangePublicPassphrase(ctx, oldPass, newPass)
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

	if !lw.IsWatchingOnlyWallet() {
		ctx, _ := lw.contextWithShutdownCancel()
		err := wallet.Unlock(ctx, privatePassphrase, nil)
		if err != nil {
			return translateError(err)
		}
		wallet.Lock()
	}

	lw.Shutdown()

	log.Info("Deleting Wallet")
	return os.RemoveAll(lw.wallet.DataDir)
}

func (mw *MultiWallet) RenameWallet(walletID int, newName string) error {
	if strings.HasPrefix(newName, "wallet-") {
		return errors.E(ErrReservedWalletName)
	}

	lw, ok := mw.libWallets[walletID]
	if ok {
		err := mw.db.One("Name", newName, &Wallet{})
		if err != nil {
			if err != storm.ErrNotFound {
				return translateError(err)
			}
		} else {
			return errors.New(ErrExist)
		}

		lw.wallet.Name = newName
		return mw.db.Save(lw.wallet) // update WalletName field
	}

	return errors.New(ErrNotExist)
}

func (mw *MultiWallet) DeleteWallet(walletID int, privPass []byte) error {
	if mw.activeSyncData != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	lw, ok := mw.libWallets[walletID]
	if ok {
		err := lw.DeleteWallet(privPass)
		if err != nil {
			return translateError(err)
		}

		err = mw.db.DeleteStruct(lw.wallet)
		if err != nil {
			return translateError(err)
		}

		delete(mw.libWallets, walletID)
		return nil
	}

	return errors.New(ErrNotExist)
}
