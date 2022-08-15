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
	btccfg "github.com/btcsuite/btcd/chaincfg"
	"github.com/decred/dcrd/chaincfg/v3"

	"github.com/planetdecred/dcrlibwallet/utils"

	"github.com/planetdecred/dcrlibwallet/wallets/btc"
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
	BTC struct {
		Wallets     map[int]*btc.Wallet
		BadWallets  map[int]*btc.Wallet
		DBDriver    string
		RootDir     string
		DB          *storm.DB
		ChainParams *btccfg.Params
	}
}

type MultiWallet struct {
	dbDriver string
	rootDir  string
	db       *storm.DB

	chainParams *chaincfg.Params
	Assets      *Assets
	wallets     map[int]*dcr.Wallet
	badWallets  map[int]*dcr.Wallet

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

	btcDB, btcRootDir, err := initializeBTCWallet(rootDir, dbDriver, netType)
	if err != nil {
		log.Errorf("error initializing BTCWallet: %s", err.Error())
		return nil, errors.Errorf("error initializing BTCWallet: %s", err.Error())
	}

	mw := &MultiWallet{
		dbDriver:    dbDriver,
		rootDir:     dcrRootDir,
		db:          dcrDB,
		chainParams: chainParams,
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
			BTC: struct {
				Wallets     map[int]*btc.Wallet
				BadWallets  map[int]*btc.Wallet
				DBDriver    string
				RootDir     string
				DB          *storm.DB
				ChainParams *btccfg.Params
			}{
				Wallets:     make(map[int]*btc.Wallet),
				BadWallets:  make(map[int]*btc.Wallet),
				DBDriver:    dbDriver,
				RootDir:     btcRootDir,
				DB:          btcDB,
				ChainParams: &btccfg.TestNet3Params,
			},
		},
	}

	mw.prepareWallets()

	mw.listenForShutdown()

	logLevel := mw.ReadStringConfigValueForKey(LogLevelConfigKey)
	SetLogLevels(logLevel)

	log.Infof("Loaded %d wallets", mw.LoadedWalletsCount())

	if err = mw.initDexClient(); err != nil {
		log.Errorf("DEX client set up error: %v", err)
	}

	return mw, nil
}

func (mw *MultiWallet) prepareWallets() error {
	// read saved wallets info from db and initialize wallets
	query := mw.db.Select(q.True()).OrderBy("ID")
	var dcrWallets []*dcr.Wallet
	err := query.Find(&dcrWallets)
	if err != nil && err != storm.ErrNotFound {
		return err
	}

	// prepare the wallets loaded from db for use
	for _, wallet := range dcrWallets {
		err = wallet.Prepare(mw.rootDir, mw.chainParams, mw.walletConfigSetFn(wallet.ID), mw.walletConfigReadFn(wallet.ID))
		if err == nil && !WalletExistsAt(wallet.DataDir) {
			err = fmt.Errorf("missing wallet database file")
		}
		if err != nil {
			mw.Assets.DCR.BadWallets[wallet.ID] = wallet
			log.Warnf("Ignored wallet load error for wallet %d (%s)", wallet.ID, wallet.Name)
		} else {
			mw.Assets.DCR.Wallets[wallet.ID] = wallet
		}

		// initialize Politeia.
		wallet.NewPoliteia("")
	}

	// read saved wallets info from db and initialize wallets
	query = mw.Assets.BTC.DB.Select(q.True()).OrderBy("ID")
	var btcWallets []*btc.Wallet
	err = query.Find(&btcWallets)
	if err != nil && err != storm.ErrNotFound {
		return err
	}

	// prepare the wallets loaded from db for use
	for _, wallet := range btcWallets {
		err = wallet.Prepare(mw.Assets.BTC.RootDir, mw.NetType(), log)
		if err == nil && !WalletExistsAt(wallet.DataDir()) {
			err = fmt.Errorf("missing wallet database file")
		}
		if err != nil {
			mw.Assets.BTC.BadWallets[wallet.ID] = wallet

			log.Warnf("Ignored wallet load error for wallet %d (%s)", wallet.ID, wallet.Name)
		} else {
			mw.Assets.BTC.Wallets[wallet.ID] = wallet
		}
	}

	return nil
}

func (mw *MultiWallet) Shutdown() {
	log.Info("Shutting down dcrlibwallet")

	// Trigger shuttingDown signal to cancel all contexts created with `shutdownContextWithCancel`.
	mw.shuttingDown <- true

	mw.shuttingDownDCR()

	mw.shuttingDownBTC()

	if logRotator != nil {
		log.Info("Shutting down log rotator")
		logRotator.Close()
		log.Info("Shutdown log rotator successfully")
	}
}

func (mw *MultiWallet) shuttingDownDCR() {
	for _, wallet := range mw.Assets.DCR.Wallets {
		wallet.CancelRescan()
	}

	for _, wallet := range mw.Assets.DCR.Wallets {
		wallet.CancelSync()
	}

	for _, wallet := range mw.Assets.DCR.Wallets {
		wallet.Shutdown()
	}

	if mw.Assets.DCR.DB != nil {
		if err := mw.Assets.DCR.DB.Close(); err != nil {
			log.Errorf("dcr db closed with error: %v", err)
		} else {
			log.Info("dcr db closed successfully")
		}
	}
}

func (mw *MultiWallet) shuttingDownBTC() {
	if mw.Assets.BTC.DB != nil {
		if err := mw.Assets.BTC.DB.Close(); err != nil {
			log.Errorf("btc db closed with error: %v", err)
		} else {
			log.Info("btc db closed successfully")
		}
	}
}

func (mw *MultiWallet) NetType() string {
	return mw.chainParams.Name
}

func (mw *MultiWallet) LogDir() string {
	return filepath.Join(mw.rootDir, logFileName)
}

func (mw *MultiWallet) TargetTimePerBlockMinutes() float64 {
	return mw.chainParams.TargetTimePerBlock.Minutes()
}

func (mw *MultiWallet) SetStartupPassphrase(passphrase []byte, passphraseType int32) error {
	return mw.ChangeStartupPassphrase([]byte(""), passphrase, passphraseType)
}

func (mw *MultiWallet) VerifyStartupPassphrase(startupPassphrase []byte) error {
	var startupPassphraseHash []byte
	err := mw.db.Get(walletsMetadataBucketName, walletstartupPassphraseField, &startupPassphraseHash)
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

	err = mw.db.Set(walletsMetadataBucketName, walletstartupPassphraseField, startupPassphraseHash)
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

	err = mw.db.Delete(walletsMetadataBucketName, walletstartupPassphraseField)
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
	err := mw.VerifyStartupPassphrase(startupPassphrase)
	if err != nil {
		return err
	}

	// Open DCR wallets
	for _, wallet := range mw.Assets.DCR.Wallets {
		if wallet.IsSyncing() {
			return errors.New(ErrSyncAlreadyInProgress)
		}
		err = wallet.OpenWallet()
		if err != nil {
			return err
		}
	}

	// Open BTC wallets
	for _, wallet := range mw.Assets.BTC.Wallets {
		err = wallet.OpenWallet()
		if err != nil {
			return err
		}
	}

	return nil
}

func (mw *MultiWallet) AllWalletsAreWatchOnly() (bool, error) {
	if len(mw.Assets.DCR.Wallets) == 0 || len(mw.Assets.BTC.Wallets) == 0 {
		return false, errors.New(ErrInvalid)
	}

	for _, w := range mw.Assets.DCR.Wallets {
		if !w.IsWatchingOnlyWallet() {
			return false, nil
		}
	}

	for _, w := range mw.Assets.BTC.Wallets {
		if !w.IsWatchingOnlyWallet() {
			return false, nil
		}
	}

	return true, nil
}

func (mw *MultiWallet) BadWallets() map[int]*dcr.Wallet {
	return mw.badWallets
}

func (mw *MultiWallet) DeleteBadWallet(walletID int) error {
	wallet := mw.badWallets[walletID]
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	log.Info("Deleting bad wallet")

	err := mw.db.DeleteStruct(wallet)
	if err != nil {
		return translateError(err)
	}

	os.RemoveAll(wallet.DataDir)
	delete(mw.badWallets, walletID)

	return nil
}

func (mw *MultiWallet) DeleteWallet(walletID int, privPass []byte, Asset string) error {
	switch Asset {
	case "DCR", "dcr":
		wallet, _ := mw.WalletWithID(walletID, "dcr")
		if wallet == nil {
			return errors.New(ErrNotExist)
		}

		if wallet.IsConnectedToDecredNetwork() {
			wallet.CancelSync()
			defer func() {
				if mw.OpenedWalletsCount() > 0 {
					wallet.SpvSync()
				}
			}()
		}

		// err := wallet.deleteWallet(privPass)
		// if err != nil {
		// 	return translateError(err)
		// }

		err := mw.Assets.DCR.DB.DeleteStruct(wallet)
		if err != nil {
			return translateError(err)
		}

		delete(mw.wallets, walletID)

		return nil
	case "BTC", "btc":
		_, wallet := mw.WalletWithID(walletID, "btc")
		if wallet == nil {
			return errors.New(ErrNotExist)
		}

		// if wallet.IsConnectedToDecredNetwork() {
		// 	wallet.CancelSync()
		// 	defer func() {
		// 		if mw.OpenedWalletsCount() > 0 {
		// 			wallet.SpvSync()
		// 		}
		// 	}()
		// }

		err := wallet.DeleteWallet(privPass)
		if err != nil {
			return translateError(err)
		}

		err = mw.Assets.BTC.DB.DeleteStruct(wallet)
		if err != nil {
			return translateError(err)
		}

		delete(mw.Assets.BTC.Wallets, walletID)

		return nil
	default:
		return nil
	}

}

func (mw *MultiWallet) WalletWithID(walletID int, Asset string) (*dcr.Wallet, *btc.Wallet) {
	switch Asset {
	case "DCR", "dcr":
		if wallet, ok := mw.Assets.DCR.Wallets[walletID]; ok {
			return wallet, nil
		}
		return nil, nil
	case "BTC", "btc":
		if wallet, ok := mw.Assets.BTC.Wallets[walletID]; ok {
			return nil, wallet
		}
		return nil, nil
	default:
		return nil, nil
	}
}

// NumWalletsNeedingSeedBackup returns the number of opened wallets whose seed haven't been verified.
func (mw *MultiWallet) NumWalletsNeedingSeedBackup() int32 {
	var backupsNeeded int32
	for _, wallet := range mw.Assets.DCR.Wallets {
		if wallet.WalletOpened() && wallet.EncryptedSeed != nil {
			backupsNeeded++
		}
	}

	for _, wallet := range mw.Assets.BTC.Wallets {
		if wallet.WalletOpened() && wallet.EncryptedSeed != nil {
			backupsNeeded++
		}
	}
	return backupsNeeded
}

func (mw *MultiWallet) LoadedWalletsCount() int32 {
	return int32(len(mw.Assets.DCR.Wallets)) + int32(len(mw.Assets.BTC.Wallets))
}

func (mw *MultiWallet) OpenedWalletIDsRaw() []int {
	walletIDs := make([]int, 0)
	for _, wallet := range mw.Assets.DCR.Wallets {
		if wallet.WalletOpened() {
			walletIDs = append(walletIDs, wallet.ID)
		}
	}

	for _, wallet := range mw.Assets.BTC.Wallets {
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

	for _, wallet := range mw.Assets.BTC.Wallets {
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

	err := mw.Assets.DCR.DB.One("Name", walletName, &dcr.Wallet{})
	if err == nil {
		return true, nil
	} else if err != storm.ErrNotFound {
		return false, err
	}

	err = mw.Assets.BTC.DB.One("Name", walletName, &btc.Wallet{})
	if err == nil {
		return true, nil
	} else if err != storm.ErrNotFound {
		return false, err
	}

	return false, nil
}
