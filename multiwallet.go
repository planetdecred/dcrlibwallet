package dcrlibwallet

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"decred.org/dcrwallet/v2/errors"
	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/planetdecred/dcrlibwallet/utils"

	"github.com/planetdecred/dcrlibwallet/wallets/dcr"

	"golang.org/x/crypto/bcrypt"
)

type Assets struct {
	DCR struct {
		Wallets     map[int]*dcr.Wallet
		BadWallets  map[int]*dcr.Wallet
		DBDriver    string
		RootDir     string
		DB          *storm.DB
		ChainParams *chaincfg.Params
	}
}

type MultiWallet struct {
	DbDriver string
	RootDir  string
	DB       *storm.DB

	ChainParams *chaincfg.Params
	Assets      *Assets

	shuttingDown chan bool
	cancelFuncs  []context.CancelFunc

	dexClient *DexClient
}

func NewMultiWallet(rootDir, dbDriver, netType, politeiaHost string) (*MultiWallet, error) {
	errors.Separator = ":: "

	chainParams, err := utils.ChainParams(netType)
	if err != nil {
		return nil, err
	}

	dcrDB, dcrRootDir, err := initializeDCRWallet(rootDir, dbDriver, netType)
	if err != nil {
		log.Errorf("error initializing DCRWallet: %s", err.Error())
		return nil, errors.Errorf("error initializing DCRWallet: %s", err.Error())
	}

	mw := &MultiWallet{
		DbDriver:    dbDriver,
		RootDir:     dcrRootDir,
		DB:          dcrDB,
		ChainParams: chainParams,
		Assets: &Assets{
			DCR: struct {
				Wallets     map[int]*dcr.Wallet
				BadWallets  map[int]*dcr.Wallet
				DBDriver    string
				RootDir     string
				DB          *storm.DB
				ChainParams *chaincfg.Params
			}{
				Wallets:     make(map[int]*dcr.Wallet),
				BadWallets:  make(map[int]*dcr.Wallet),
				DBDriver:    dbDriver,
				RootDir:     dcrRootDir,
				DB:          dcrDB,
				ChainParams: chainParams,
			},
		},
	}

	// read saved wallets info from db and initialize wallets
	query := mw.DB.Select(q.True()).OrderBy("ID")
	var wallets []*dcr.Wallet
	err = query.Find(&wallets)
	if err != nil && err != storm.ErrNotFound {
		return nil, err
	}

	// prepare the wallets loaded from db for use
	for _, wallet := range wallets {
		err = wallet.Prepare(mw.RootDir, mw.ChainParams, mw.walletConfigSetFn(wallet.ID), mw.walletConfigReadFn(wallet.ID))
		if err == nil && !WalletExistsAt(wallet.DataDir) {
			err = fmt.Errorf("missing wallet database file")
		}
		if err != nil {
			mw.Assets.DCR.BadWallets[wallet.ID] = wallet
			log.Warnf("Ignored wallet load error for wallet %d (%s)", wallet.ID, wallet.Name)
		} else {
			mw.Assets.DCR.Wallets[wallet.ID] = wallet
		}

		logLevel := wallet.ReadStringConfigValueForKey(LogLevelConfigKey, "")
		SetLogLevels(logLevel)

		// initialize Politeia.
		wallet.NewPoliteia(politeiaHost)
	}

	mw.listenForShutdown()

	logLevel := mw.ReadStringConfigValueForKey(LogLevelConfigKey)
	SetLogLevels(logLevel)

	log.Infof("Loaded %d wallets", mw.LoadedWalletsCount())

	if err = mw.initDexClient(); err != nil {
		log.Errorf("DEX client set up error: %v", err)
	}

	return mw, nil
}

func (mw *MultiWallet) Shutdown() {
	log.Info("Shutting down dcrlibwallet")

	// Trigger shuttingDown signal to cancel all contexts created with `shutdownContextWithCancel`.
	mw.shuttingDown <- true

	for _, wallet := range mw.Assets.DCR.Wallets {
		wallet.CancelRescan()
	}

	for _, wallet := range mw.Assets.DCR.Wallets {
		wallet.CancelSync()
	}

	for _, wallet := range mw.Assets.DCR.Wallets {
		wallet.Shutdown()
	}

	if mw.DB != nil {
		if err := mw.DB.Close(); err != nil {
			log.Errorf("db closed with error: %v", err)
		} else {
			log.Info("db closed successfully")
		}
	}

	if logRotator != nil {
		log.Info("Shutting down log rotator")
		logRotator.Close()
		log.Info("Shutdown log rotator successfully")
	}
}

func (mw *MultiWallet) NetType() string {
	return mw.ChainParams.Name
}

func (mw *MultiWallet) LogDir() string {
	return filepath.Join(mw.RootDir, logFileName)
}

func (mw *MultiWallet) TargetTimePerBlockMinutes() float64 {
	return mw.ChainParams.TargetTimePerBlock.Minutes()
}

func (mw *MultiWallet) SetStartupPassphrase(passphrase []byte, passphraseType int32) error {
	return mw.ChangeStartupPassphrase([]byte(""), passphrase, passphraseType)
}

func (mw *MultiWallet) VerifyStartupPassphrase(startupPassphrase []byte) error {
	var startupPassphraseHash []byte
	err := mw.DB.Get(walletsMetadataBucketName, walletstartupPassphraseField, &startupPassphraseHash)
	if err != nil && err != storm.ErrNotFound {
		return err
	}

	if startupPassphraseHash == nil {
		// startup passphrase was not previously set
		if len(startupPassphrase) > 0 {
			return errors.E(ErrInvalidPassphrase)
		}
		return nil
	}

	// startup passphrase was set, verify
	err = bcrypt.CompareHashAndPassword(startupPassphraseHash, startupPassphrase)
	if err != nil {
		return errors.E(ErrInvalidPassphrase)
	}

	return nil
}

func (mw *MultiWallet) ChangeStartupPassphrase(oldPassphrase, newPassphrase []byte, passphraseType int32) error {
	if len(newPassphrase) == 0 {
		return mw.RemoveStartupPassphrase(oldPassphrase)
	}

	err := mw.VerifyStartupPassphrase(oldPassphrase)
	if err != nil {
		return err
	}

	startupPassphraseHash, err := bcrypt.GenerateFromPassword(newPassphrase, bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	err = mw.DB.Set(walletsMetadataBucketName, walletstartupPassphraseField, startupPassphraseHash)
	if err != nil {
		return err
	}

	mw.SaveUserConfigValue(IsStartupSecuritySetConfigKey, true)
	mw.SaveUserConfigValue(StartupSecurityTypeConfigKey, passphraseType)

	return nil
}

func (mw *MultiWallet) RemoveStartupPassphrase(oldPassphrase []byte) error {
	err := mw.VerifyStartupPassphrase(oldPassphrase)
	if err != nil {
		return err
	}

	err = mw.DB.Delete(walletsMetadataBucketName, walletstartupPassphraseField)
	if err != nil {
		return err
	}

	mw.SaveUserConfigValue(IsStartupSecuritySetConfigKey, false)
	mw.DeleteUserConfigValueForKey(StartupSecurityTypeConfigKey)

	return nil
}

func (mw *MultiWallet) IsStartupSecuritySet() bool {
	return mw.ReadBoolConfigValueForKey(IsStartupSecuritySetConfigKey, false)
}

func (mw *MultiWallet) StartupSecurityType() int32 {
	return mw.ReadInt32ConfigValueForKey(StartupSecurityTypeConfigKey, PassphraseTypePass)
}

func (mw *MultiWallet) OpenWallets(startupPassphrase []byte) error {
	// if mw.IsSyncing() {
	// 	return errors.New(ErrSyncAlreadyInProgress)
	// }

	err := mw.VerifyStartupPassphrase(startupPassphrase)
	if err != nil {
		return err
	}

	for _, wallet := range mw.Assets.DCR.Wallets {
		err = wallet.OpenWallet()
		if err != nil {
			return err
		}
	}

	return nil
}

func (mw *MultiWallet) AllWalletsAreWatchOnly() (bool, error) {
	if len(mw.Assets.DCR.Wallets) == 0 {
		return false, errors.New(ErrInvalid)
	}

	for _, w := range mw.Assets.DCR.Wallets {
		if !w.IsWatchingOnlyWallet() {
			return false, nil
		}
	}

	return true, nil
}

func (mw *MultiWallet) BadWallets() map[int]*dcr.Wallet {
	return mw.Assets.DCR.BadWallets
}

func (mw *MultiWallet) DeleteBadWallet(walletID int) error {
	wallet := mw.Assets.DCR.BadWallets[walletID]
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	log.Info("Deleting bad wallet")

	err := mw.DB.DeleteStruct(wallet)
	if err != nil {
		return translateError(err)
	}

	os.RemoveAll(wallet.DataDir)
	delete(mw.Assets.DCR.BadWallets, walletID)

	return nil
}

func (mw *MultiWallet) WalletWithID(walletID int) *dcr.Wallet {
	if wallet, ok := mw.Assets.DCR.Wallets[walletID]; ok {
		return wallet
	}
	return nil
}

// NumWalletsNeedingSeedBackup returns the number of opened wallets whose seed haven't been verified.
func (mw *MultiWallet) NumWalletsNeedingSeedBackup() int32 {
	var backupsNeeded int32
	for _, wallet := range mw.Assets.DCR.Wallets {
		if wallet.WalletOpened() && wallet.EncryptedSeed != nil {
			backupsNeeded++
		}
	}
	return backupsNeeded
}

func (mw *MultiWallet) LoadedWalletsCount() int32 {
	return int32(len(mw.Assets.DCR.Wallets))
}

func (mw *MultiWallet) OpenedWalletIDsRaw() []int {
	walletIDs := make([]int, 0)
	for _, wallet := range mw.Assets.DCR.Wallets {
		if wallet.WalletOpened() {
			walletIDs = append(walletIDs, wallet.ID)
		}
	}
	return walletIDs
}

func (mw *MultiWallet) OpenedWalletIDs() string {
	walletIDs := mw.OpenedWalletIDsRaw()
	jsonEncoded, _ := json.Marshal(&walletIDs)
	return string(jsonEncoded)
}

func (mw *MultiWallet) OpenedWalletsCount() int32 {
	return int32(len(mw.OpenedWalletIDsRaw()))
}

func (mw *MultiWallet) SyncedWalletsCount() int32 {
	var syncedWallets int32
	for _, wallet := range mw.Assets.DCR.Wallets {
		if wallet.WalletOpened() && wallet.Synced {
			syncedWallets++
		}
	}

	return syncedWallets
}

func (mw *MultiWallet) WalletNameExists(walletName string) (bool, error) {
	if strings.HasPrefix(walletName, "wallet-") {
		return false, errors.E(ErrReservedWalletName)
	}

	err := mw.DB.One("Name", walletName, &dcr.Wallet{})
	if err == nil {
		return true, nil
	} else if err != storm.ErrNotFound {
		return false, err
	}

	return false, nil
}
