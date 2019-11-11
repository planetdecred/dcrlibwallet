package dcrlibwallet

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/decred/dcrwallet/errors/v2"
	wallet "github.com/decred/dcrwallet/wallet/v3"
	"github.com/raedahgroup/dcrlibwallet/internal/netparams"
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
	configDB *storm.DB

	activeNet  *netparams.Params
	libWallets map[int]*LibWallet
	*syncData

	shuttingDown chan bool
	cancelFuncs  []context.CancelFunc
}

func NewMultiWallet(rootDir, dbDriver, netType string) (*MultiWallet, error) {
	rootDir = filepath.Join(rootDir, netType)
	initLogRotator(filepath.Join(rootDir, logFileName))
	errors.Separator = ":: "

	activeNet := utils.NetParams(netType)
	if activeNet == nil {
		return nil, errors.E("unsupported network type: %s", netType)
	}

	configDbPath := filepath.Join(rootDir, userConfigDbFilename)
	configDB, err := storm.Open(configDbPath)
	if err != nil {
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			return nil, errors.E("settings db is in use by another process")
		}
		return nil, errors.E("error opening settings db store: %s", err.Error())
	}

	walletsDb, err := storm.Open(filepath.Join(rootDir, walletsDbName))
	if err != nil {
		log.Errorf("Error opening wallet database: %s", err.Error())
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			return nil, errors.E("wallet database is in use by another process")
		}
		return nil, errors.E("error opening wallet index database: %s", err.Error())
	}

	// init database for saving/reading wallet objects
	err = walletsDb.Init(&Wallet{})
	if err != nil {
		log.Errorf("Error initializing wallet database: %s", err.Error())
		return nil, err
	}

	syncData := &syncData{
		syncCanceled:          make(chan bool),
		syncProgressListeners: make(map[string]SyncProgressListener),
	}

	mw := &MultiWallet{
		dbDriver:   dbDriver,
		rootDir:    rootDir,
		db:         walletsDb,
		configDB:   configDB,
		activeNet:  activeNet,
		libWallets: make(map[int]*LibWallet),
		syncData:   syncData,
	}

	mw.listenForShutdown()

	err = mw.loadWallets()
	if err != nil {
		return nil, err
	}

	log.Infof("Loaded %d wallets", mw.LoadedWalletsCount())

	return mw, nil
}

func (mw *MultiWallet) Shutdown() {
	log.Info("Shutting down dcrlibwallet")

	// Trigger shuttingDown signal to cancel all contexts created with `contextWithShutdownCancel`.
	mw.shuttingDown <- true

	mw.CancelSync()

	for _, lw := range mw.libWallets {
		lw.Shutdown()
	}

	if logRotator != nil {
		log.Info("Shutting down log rotator")
		logRotator.Close()
	}

	if mw.db != nil {
		if err := mw.db.Close(); err != nil {
			log.Errorf("db closed with error: %v", err)
		} else {
			log.Info("db closed successfully")
		}
	}
}

func (mw *MultiWallet) loadWallets() error {
	query := mw.db.Select(q.True()).OrderBy("ID")
	var wallets []Wallet
	err := query.Find(&wallets)
	if err != nil && err != storm.ErrNotFound {
		return err
	}

	mw.libWallets = make(map[int]*LibWallet)
	for _, lw := range wallets {
		lw.walletDbDriver = mw.dbDriver
		lw.netType = mw.activeNet.Name
		libWallet, err := NewLibWallet(&lw)
		if err != nil {
			return err
		}

		mw.libWallets[lw.ID] = libWallet
	}

	return nil
}

func (mw *MultiWallet) GetNeededBackupCount() int32 {
	var backupsNeeded int32
	for _, lw := range mw.libWallets {
		if lw.WalletOpened() && lw.wallet.Seed != "" {
			backupsNeeded++
		}
	}

	return backupsNeeded
}

func (mw *MultiWallet) LoadedWalletsCount() int32 {
	return int32(len(mw.libWallets))
}

func (mw *MultiWallet) OpenedWalletIDsRaw() []int {
	wallets := make([]int, 0)
	for _, lw := range mw.libWallets {
		if lw.WalletOpened() {
			wallets = append(wallets, lw.wallet.ID)
		}
	}

	return wallets
}

func (mw *MultiWallet) OpenedWalletIDs() string {
	wallets := mw.OpenedWalletIDsRaw()
	jsonEncoded, _ := json.Marshal(&wallets)

	return string(jsonEncoded)
}

func (mw *MultiWallet) OpenedWalletsCount() int32 {
	return int32(len(mw.OpenedWalletIDsRaw()))
}

func (mw *MultiWallet) SyncedWalletsCount() int32 {
	var syncedWallets int32
	for _, lw := range mw.libWallets {
		if lw.WalletOpened() && lw.synced {
			syncedWallets++
		}
	}

	return syncedWallets
}

func (mw *MultiWallet) CreateNewWallet(privatePassphrase string, privatePassphraseType int32) (*LibWallet, error) {

	if mw.activeSyncData != nil {
		return nil, errors.New(ErrSyncAlreadyInProgress)
	}

	seed, err := GenerateSeed()
	if err != nil {
		return nil, err
	}

	properties := &Wallet{
		Seed:                  seed,
		PrivatePassphraseType: privatePassphraseType,
		DiscoveredAccounts:    true,
	}

	return mw.createWallet(properties, seed, privatePassphrase)
}

func (mw *MultiWallet) CreateWatchOnlyWallet(walletName string, extendedPublicKey string) (*LibWallet, error) {

	exists, err := mw.WalletNameExists(walletName)
	if err != nil {
		return nil, err
	} else if exists {
		return nil, errors.New(ErrNotExist)
	}

	w := &Wallet{
		Name:               walletName,
		DiscoveredAccounts: true,
	}

	libWallet, err := mw.saveWalletToDatabase(w)
	if err != nil {
		return nil, err
	}

	err = libWallet.CreateWatchingOnlyWallet(wallet.InsecurePubPassphrase, extendedPublicKey)
	if err != nil {
		return nil, err
	}

	go mw.listenForTransactions(libWallet)

	return libWallet, nil
}

func (mw *MultiWallet) RestoreWallet(seedMnemonic, privatePassphrase string, privatePassphraseType int32) (*LibWallet, error) {
	if mw.activeSyncData != nil {
		return nil, errors.New(ErrSyncAlreadyInProgress)
	}

	properties := &Wallet{
		PrivatePassphraseType: privatePassphraseType,
		DiscoveredAccounts:    false,
	}

	return mw.createWallet(properties, seedMnemonic, privatePassphrase)
}

func (mw *MultiWallet) createWallet(w *Wallet, seedMnemonic, privatePassphrase string) (*LibWallet, error) {

	libWallet, err := mw.saveWalletToDatabase(w)
	if err != nil {
		return nil, err
	}

	err = libWallet.CreateWallet(privatePassphrase, seedMnemonic)
	if err != nil {
		return nil, err
	}

	go mw.listenForTransactions(libWallet)

	return libWallet, nil
}

func (mw *MultiWallet) saveWalletToDatabase(w *Wallet) (*LibWallet, error) {
	// saving struct to update ID property with an autogenerated value
	err := mw.db.Save(w)
	if err != nil {
		return nil, err
	}

	// delete from database if not created successfully
	defer func() {
		if err != nil {
			mw.db.DeleteStruct(w)
		}
	}()

	walletDataDir := filepath.Join(mw.rootDir, strconv.Itoa(w.ID))
	os.MkdirAll(walletDataDir, os.ModePerm) // create wallet dir

	w.DataDir = walletDataDir
	err = mw.db.Save(w) // update database with complete wallet information
	if err != nil {
		return nil, err
	}

	w.walletDbDriver = mw.dbDriver
	w.netType = mw.activeNet.Name
	libWallet, err := NewLibWallet(w)
	if err != nil {
		return nil, err
	}

	mw.libWallets[w.ID] = libWallet
	return libWallet, nil
}

func (mw *MultiWallet) WalletNameExists(walletName string) (bool, error) {

	if strings.HasPrefix(walletName, "wallet-") {
		return false, errors.E(ErrReservedWalletName)
	}

	err := mw.db.One("Name", walletName, &Wallet{})
	if err == nil {
		return true, nil
	} else if err != storm.ErrNotFound {
		return false, err
	}

	return false, nil
}

func (mw *MultiWallet) GetWallet(walletID int) *LibWallet {
	if lw, exists := mw.libWallets[walletID]; exists {
		return lw
	}
	return nil
}

func (mw *MultiWallet) OpenWallets(pubPass []byte) error {
	if mw.activeSyncData != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	for _, lw := range mw.libWallets {
		err := lw.OpenWallet(pubPass)
		if err != nil {
			return err
		}

		go mw.listenForTransactions(lw)
	}

	return nil
}

func (mw *MultiWallet) OpenWallet(walletID int, pubPass []byte) error {
	if mw.activeSyncData != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}
	libWallet, ok := mw.libWallets[walletID]
	if ok {
		err := libWallet.OpenWallet(pubPass)
		if err != nil {
			return err
		}

		go mw.listenForTransactions(libWallet)
		return nil
	}

	return errors.New(ErrNotExist)
}

func (mw *MultiWallet) UnlockWallet(walletID int, privPass []byte) error {
	w, ok := mw.libWallets[walletID]
	if ok {
		return w.UnlockWallet(privPass)
	}

	return errors.New(ErrNotExist)
}

func (mw *MultiWallet) discoveredAccounts(walletID int) error {
	var w LibWallet
	err := mw.db.One("ID", walletID, &w)
	if err != nil {
		return err
	}

	w.wallet.DiscoveredAccounts = true
	err = mw.db.Save(&w)
	if err != nil {
		return err
	}

	mw.libWallets[walletID].wallet.DiscoveredAccounts = true
	return nil
}

func (mw *MultiWallet) setNetworkBackend(netBakend wallet.NetworkBackend) {
	for _, w := range mw.libWallets {
		if w.WalletOpened() {
			w.wallet.SetNetworkBackend(netBakend)
		}
	}
}
