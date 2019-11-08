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

	activeNet *netparams.Params
	wallets   map[int]*LibWallet
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

	err := os.MkdirAll(rootDir, 0700)
	if err != nil {
		return nil, errors.E("failed to create wallet db directory: %v", err)
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

	db, err := storm.Open(filepath.Join(rootDir, walletsDbName))
	if err != nil {
		log.Errorf("Error opening wallet database: %s", err.Error())
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			return nil, errors.E("wallet database is in use by another process")
		}
		return nil, errors.E("error opening wallet index database: %s", err.Error())
	}

	// init database for saving/reading wallet properties objects
	err = db.Init(&Properties{})
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
		configDB:  configDB,
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
	query := mw.db.Select(q.True()).OrderBy("ID")
	var propertiesArray []Properties
	err := query.Find(&propertiesArray)
	if err != nil && err != storm.ErrNotFound {
		return 0, err
	}

	mw.wallets = make(map[int]*LibWallet)
	for _, p := range propertiesArray {
		libWallet, err := NewLibWallet(&p, mw.dbDriver, mw.activeNet.Name)
		if err != nil {
			return 0, err
		}

		mw.wallets[p.ID] = libWallet
	}

	return len(propertiesArray), nil
}

func (mw *MultiWallet) GetBackupsNeeded() int32 {
	var backupsNeeded int32
	for _, w := range mw.wallets {
		if w.WalletOpened() && w.Seed != "" {
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
			wallets = append(wallets, w.ID)
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

func (mw *MultiWallet) CreateNewWallet(privatePassphrase string, spendingPassphraseType int32) (*LibWallet, error) {

	if mw.activeSyncData != nil {
		return nil, errors.New(ErrSyncAlreadyInProgress)
	}

	seed, err := GenerateSeed()
	if err != nil {
		return nil, err
	}

	properties := &Properties{
		Seed:                   seed,
		SpendingPassphraseType: spendingPassphraseType,
		DiscoveredAccounts:     true,
	}

	return mw.createWallet(properties, seed, privatePassphrase)
}

func (mw *MultiWallet) CreateWatchOnlyWallet(walletName string, extendedPublicKey string) (*LibWallet, error) {

	exists, err := mw.WalletNameExists(walletName)
	if err != nil {
		return nil, err
	} else if exists {
		return nil, errors.New(ErrWalletNotLoaded)
	}

	props := &Properties{
		Name:               walletName,
		DiscoveredAccounts: true,
	}

	err = mw.db.Save(props)
	if err != nil {
		return nil, err
	}

	homeDir := filepath.Join(mw.rootDir, strconv.Itoa(props.ID))
	os.MkdirAll(homeDir, os.ModePerm) // create wallet dir

	props.DataDir = homeDir
	err = mw.db.Save(props) // update database with complete wallet information
	if err != nil {
		return nil, err
	}

	// delete from database if not created successfully
	defer func() {
		if err != nil {
			mw.db.DeleteStruct(props)
		}
	}()

	libWallet, err := NewLibWallet(props, mw.dbDriver, mw.activeNet.Name)
	if err != nil {
		return nil, err
	}

	mw.wallets[props.ID] = libWallet

	err = libWallet.CreateWatchingOnlyWallet(wallet.InsecurePubPassphrase, extendedPublicKey)
	if err != nil {
		return nil, err
	}

	go mw.listenForTransactions(libWallet)

	return libWallet, nil
}

func (mw *MultiWallet) RestoreWallet(seedMnemonic, privatePassphrase string, spendingPassphraseType int32) (*LibWallet, error) {
	if mw.activeSyncData != nil {
		return nil, errors.New(ErrSyncAlreadyInProgress)
	}

	properties := &Properties{
		SpendingPassphraseType: spendingPassphraseType,
		DiscoveredAccounts:     false,
	}

	return mw.createWallet(properties, seedMnemonic, privatePassphrase)
}

func (mw *MultiWallet) createWallet(properties *Properties, seedMnemonic, privatePassphrase string) (*LibWallet, error) {

	err := mw.db.Save(properties)
	if err != nil {
		return nil, err
	}

	walletName := "wallet-" + strconv.Itoa(properties.ID) // wallet-#

	homeDir := filepath.Join(mw.rootDir, strconv.Itoa(properties.ID))
	os.MkdirAll(homeDir, os.ModePerm) // create wallet dir

	// update database wallet data dir
	properties.DataDir = homeDir
	properties.Name = walletName
	err = mw.db.Save(properties) // update database with complete wallet information
	if err != nil {
		return nil, err
	}

	// delete from database if not created successfully
	defer func() {
		if err != nil {
			mw.db.DeleteStruct(properties)
		}
	}()

	libWallet, err := NewLibWallet(properties, mw.dbDriver, mw.activeNet.Name)
	if err != nil {
		return nil, err
	}

	libWallet.Properties = properties
	mw.wallets[properties.ID] = libWallet

	err = libWallet.CreateWallet(privatePassphrase, seedMnemonic)
	if err != nil {
		return nil, err
	}

	go mw.listenForTransactions(libWallet)

	return libWallet, nil
}

func (mw *MultiWallet) WalletNameExists(walletName string) (bool, error) {

	if strings.HasPrefix(walletName, "wallet-") {
		return false, errors.E(ErrReservedWalletName)
	}

	err := mw.db.One("Name", walletName, &Properties{})
	if err == nil {
		return true, nil
	} else if err != storm.ErrNotFound {
		return false, err
	}

	return false, nil
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

		go mw.listenForTransactions(w)
	}

	return nil
}

func (mw *MultiWallet) OpenWallet(walletID int, pubPass []byte) error {
	if mw.activeSyncData != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}
	wallet, ok := mw.wallets[walletID]
	if ok {
		err := wallet.OpenWallet(pubPass)
		if err != nil {
			return err
		}

		go mw.listenForTransactions(wallet)
		return nil
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
	err := mw.db.One("ID", walletID, &w)
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
		}
	}
}
