package dcrlibwallet

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/netparams"
	wallet "github.com/decred/dcrwallet/wallet/v3"
	"github.com/raedahgroup/dcrlibwallet/utils"
	bolt "go.etcd.io/bbolt"
)

const (
	logFileName   = "dcrlibwallet.log"
	walletsDbName = "wallets.db"
)

type MultiWallet struct {
	dbDriver string
	rootDir  string
	db       *storm.DB

	activeNet *netparams.Params
	wallets   map[int]*LibWallet
	*syncData

	shuttingDown chan bool
	cancelFuncs  []context.CancelFunc
}

func NewMultiWallet(rootDir, dbDriver, netType string) (*MultiWallet, error) {

	errors.Separator = ":: "

	activeNet := utils.NetParams(netType)
	if activeNet == nil {
		return nil, fmt.Errorf("unsupported network type: %s", netType)
	}

	rootDir = filepath.Join(rootDir, netType)

	initLogRotator(filepath.Join(rootDir, logFileName))

	db, err := storm.Open(filepath.Join(rootDir, walletsDbName))
	if err != nil {
		log.Errorf("Error opening wallet database: %s", err.Error())
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			return nil, fmt.Errorf("wallet database is in use by another process")
		}
		return nil, fmt.Errorf("error opening wallet index database: %s", err.Error())
	}

	// init database for saving/reading wallet objects
	err = db.Init(&LibWallet{})
	if err != nil {
		log.Errorf("Error initializing wallet database: %s", err.Error())
		return nil, err
	}

	syncData := &syncData{
		syncCanceled:          make(chan bool),
		syncProgressListeners: make(map[string]SyncProgressListener),
	}

	mw := &MultiWallet{
		dbDriver:  dbDriver,
		rootDir:   rootDir,
		db:        db,
		activeNet: activeNet,
		wallets:   make(map[int]*LibWallet),
		syncData:  syncData,
	}

	mw.listenForShutdown()

	loadedWallets, err := mw.loadWallets()
	if err != nil {
		return nil, err
	}

	log.Infof("Loaded %d wallets", loadedWallets)

	return mw, nil
}

func (mw *MultiWallet) Shutdown() {
	log.Info("Shutting down dcrlibwallet")

	// Trigger shuttingDown signal to cancel all contexts created with `contextWithShutdownCancel`.
	mw.shuttingDown <- true

	mw.CancelSync()

	for _, w := range mw.wallets {
		w.Shutdown()
	}

	if logRotator != nil {
		log.Info("Shutting down log rotator")
		logRotator.Close()
	}

	if mw.db != nil {
		err := mw.db.Close()
		if err != nil {
			log.Errorf("db closed with error: %v", err)
		} else {
			log.Info("db closed successfully")
		}
	}
}

func (mw *MultiWallet) loadWallets() (int, error) {
	query := mw.db.Select(q.True()).OrderBy("WalletID")
	var wallets []LibWallet
	err := query.Find(&wallets)
	if err != nil && err != storm.ErrNotFound {
		return 0, err
	}

	mw.wallets = make(map[int]*LibWallet)
	for _, w := range wallets {
		libWallet, err := NewLibWallet(w.WalletDataDir, mw.dbDriver, mw.activeNet.Name)
		if err != nil {
			return 0, err
		}

		libWallet.WalletProperties = w.WalletProperties
		mw.wallets[w.WalletID] = libWallet
	}

	return len(wallets), nil
}

func (mw *MultiWallet) GetBackupsNeeded() int32 {
	var backupsNeeded int32
	for _, w := range mw.wallets {
		if w.WalletOpened() && w.WalletSeed != "" {
			backupsNeeded++
		}
	}

	return backupsNeeded
}

func (mw *MultiWallet) LoadedWalletsCount() int32 {
	return int32(len(mw.wallets))
}

func (mw *MultiWallet) OpenedWalletsRaw() []int {
	wallets := make([]int, 0)
	for _, w := range mw.wallets {
		if w.WalletOpened() {
			wallets = append(wallets, w.WalletID)
		}
	}

	return wallets
}

func (mw *MultiWallet) OpenedWallets() string {
	wallets := mw.OpenedWalletsRaw()
	jsonEncoded, _ := json.Marshal(&wallets)

	return string(jsonEncoded)
}

func (mw *MultiWallet) OpenedWalletsCount() int32 {
	return int32(len(mw.OpenedWalletsRaw()))
}

func (mw *MultiWallet) SyncedWalletCount() int32 {
	var syncedWallet int32
	for _, w := range mw.wallets {
		if w.WalletOpened() && w.synced {
			syncedWallet++
		}
	}

	return syncedWallet
}

func (mw *MultiWallet) CreateNewWallet(passphrase string, spendingPassphraseType int32) (*LibWallet, error) {

	if mw.activeSyncData != nil {
		return nil, errors.New(ErrSyncAlreadyInProgress)
	}

	seed, err := GenerateSeed()
	if err != nil {
		return nil, err
	}

	properties := WalletProperties{
		WalletSeed:             seed,
		SpendingPassphraseType: spendingPassphraseType,
		DiscoveredAccounts:     true,
		DefaultAccount:         0,
	}

	return mw.createWallet(properties, seed, passphrase)
}

func (mw *MultiWallet) RestoreWallet(seedMnemonic, passphrase string, spendingPassphraseType int32) (*LibWallet, error) {
	if mw.activeSyncData != nil {
		return nil, errors.New(ErrSyncAlreadyInProgress)
	}

	properties := WalletProperties{
		SpendingPassphraseType: spendingPassphraseType,
		DiscoveredAccounts:     false,
		DefaultAccount:         0,
	}

	return mw.createWallet(properties, seedMnemonic, passphrase)
}

func (mw *MultiWallet) createWallet(properties WalletProperties, seedMnemonic, passphrase string) (*LibWallet, error) {
	lw := &LibWallet{
		WalletProperties: properties,
	}

	err := mw.db.Save(lw)
	if err != nil {
		return nil, err
	}

	walletID := lw.WalletID
	walletName := "wallet-" + strconv.Itoa(walletID) // wallet-#

	homeDir := filepath.Join(mw.rootDir, strconv.Itoa(walletID))
	os.MkdirAll(homeDir, os.ModePerm) // create wallet dir

	// update database wallet data dir
	lw.WalletDataDir = homeDir
	lw.WalletName = walletName
	err = mw.db.Save(lw) // updating database with new complete wallet information
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			mw.db.DeleteStruct(lw)
		}

	}()

	libWallet, err := NewLibWallet(homeDir, mw.dbDriver, mw.activeNet.Name)
	if err != nil {
		return nil, err
	}

	libWallet.WalletProperties = lw.WalletProperties
	mw.wallets[walletID] = libWallet

	err = libWallet.CreateWallet(passphrase, seedMnemonic)
	if err != nil {
		return nil, err
	}

	return libWallet, nil
}

func (mw *MultiWallet) GetWallet(walletID int) *LibWallet {
	w := mw.wallets[walletID]
	return w
}

func (mw *MultiWallet) OpenWallets(pubPass []byte) error {
	if mw.activeSyncData != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	for _, w := range mw.wallets {
		err := w.OpenWallet(pubPass)
		if err != nil {
			return err
		}
	}

	return nil
}

func (mw *MultiWallet) OpenWallet(walletID int, pubPass []byte) error {
	if mw.activeSyncData != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}
	wallet, ok := mw.wallets[walletID]
	if ok {
		return wallet.OpenWallet(pubPass)
	}

	return errors.New(ErrNotExist)
}

func (mw *MultiWallet) UnlockWallet(walletID int, privPass []byte) error {
	w, ok := mw.wallets[walletID]
	if ok {
		return w.UnlockWallet(privPass)
	}

	return errors.New(ErrNotExist)
}

func (mw *MultiWallet) discoveredAccounts(walletID int) error {
	var w LibWallet
	err := mw.db.One("WalletID", walletID, &w)
	if err != nil {
		return err
	}

	w.DiscoveredAccounts = true
	err = mw.db.Save(&w)
	if err != nil {
		return err
	}

	mw.wallets[walletID].DiscoveredAccounts = true
	return nil
}

func (mw *MultiWallet) setNetworkBackend(netBakend wallet.NetworkBackend) {
	for _, w := range mw.wallets {
		if w.WalletOpened() {
			w.wallet.SetNetworkBackend(netBakend)
			w.walletLoader.SetNetworkBackend(netBakend)
		}
	}
}
