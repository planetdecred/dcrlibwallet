package dcrlibwallet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/asdine/storm"
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
	wallets   map[string]*LibWallet
	*syncData

	shuttingDown chan bool
	cancelFuncs  []context.CancelFunc
}

func NewMultiWallet(rootDir, dbDriver, netType string) (*MultiWallet, error) {
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
		wallets:   make(map[string]*LibWallet),
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
	var wallets []LibWallet
	err := mw.db.All(&wallets)
	if err != nil && err != storm.ErrNotFound {
		return 0, err
	}

	mw.wallets = make(map[string]*LibWallet)
	for _, w := range wallets {
		libWallet, err := NewLibWallet(w.WalletDataDir, mw.dbDriver, mw.activeNet.Name)
		if err != nil {
			return 0, err
		}

		mw.wallets[w.WalletAlias] = libWallet
	}

	return len(wallets), nil
}

func (mw *MultiWallet) LoadedWalletsCount() int32 {
	return int32(len(mw.wallets))
}

func (mw *MultiWallet) OpenedWallets() []string {
	wallets := make([]string, 0)
	for _, w := range mw.wallets {
		if w.WalletOpened() {
			wallets = append(wallets, w.WalletAlias)
		}
	}

	return wallets
}

func (mw *MultiWallet) OpenedWalletsCount() int32 {
	return int32(len(mw.OpenedWallets()))
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

func (mw *MultiWallet) CreateNewWallet(walletAlias, passphrase, seedMnemonic string) (*LibWallet, error) {
	err := mw.db.One("WalletAlias", walletAlias, &LibWallet{})
	if err != nil {
		if err != storm.ErrNotFound {
			return nil, err
		}
	} else {
		log.Infof("Wallet alias exists: %s", walletAlias)
		return nil, errors.New(ErrExist)
	}

	homeDir := filepath.Join(mw.rootDir, walletAlias)
	os.MkdirAll(homeDir, os.ModePerm) // create wallet dir
	lw, err := NewLibWallet(homeDir, mw.dbDriver, mw.activeNet.Name)
	if err != nil {
		return nil, err
	}
	lw.WalletAlias = walletAlias
	lw.WalletSeed = seedMnemonic

	err = mw.db.Save(lw)
	if err != nil {
		return nil, err
	}

	mw.wallets[walletAlias] = lw

	err = lw.CreateWallet(passphrase, seedMnemonic)
	if err != nil {
		return nil, err
	}

	return lw, nil
}

func (mw *MultiWallet) GetWallet(walletAlias string) *LibWallet {
	w := mw.wallets[walletAlias]
	return w
}

func (mw *MultiWallet) OpenWallets(pubPass []byte) error {
	for _, w := range mw.wallets {
		err := w.OpenWallet(pubPass)
		if err != nil {
			return err
		}
	}

	return nil
}

func (mw *MultiWallet) OpenWallet(walletAlias string, pubPass []byte) error {
	wallet, ok := mw.wallets[walletAlias]
	if ok {
		return wallet.OpenWallet(pubPass)
	}

	return errors.New(ErrNotExist)
}

func (mw *MultiWallet) UnlockWallet(walletAlias string, privPass []byte) error {
	w, ok := mw.wallets[walletAlias]
	if ok {
		return w.UnlockWallet(privPass)
	}

	return errors.New(ErrNotExist)
}

func (mw *MultiWallet) setNetworkBackend(netBakend wallet.NetworkBackend) {
	for _, w := range mw.wallets {
		w.wallet.SetNetworkBackend(netBakend)
		w.walletLoader.SetNetworkBackend(netBakend)
	}
}
