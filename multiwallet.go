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
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrwallet/errors/v2"
	w "github.com/decred/dcrwallet/wallet/v3"
	"github.com/raedahgroup/dcrlibwallet/internal/snacl"
	"github.com/raedahgroup/dcrlibwallet/utils"
	bolt "go.etcd.io/bbolt"
)

type MultiWallet struct {
	dbDriver string
	rootDir  string
	db       *storm.DB

	// publicEncryptionKeyParams is used with the cross-wallet public
	// passphrase to re-construct the publicEncryptionKey.
	publicEncryptionKeyParams []byte

	// publicEncryptionKey should be used to encrypt information
	// that left exposed would present a privacy risk but cannot
	// be used to move funds out of any of the wallets.
	// The shared wallet public passphrase is required to derive
	// this encryption key before it can be used for encryption and
	// decryption.  This derivation is done when `OpenWallets` is
	// called with a valid public passphrase.
	publicEncryptionKey *snacl.SecretKey

	chainParams *chaincfg.Params
	wallets     map[int]*Wallet
	syncData    *syncData

	txAndBlockNotificationListeners map[string]TxAndBlockNotificationListener
	blocksRescanProgressListener    BlocksRescanProgressListener

	shuttingDown chan bool
	cancelFuncs  []context.CancelFunc
}

func NewMultiWallet(rootDir, dbDriver, netType string) (*MultiWallet, error) {
	rootDir = filepath.Join(rootDir, netType)
	initLogRotator(filepath.Join(rootDir, logFileName))
	errors.Separator = ":: "

	chainParams := utils.ChainParams(netType)
	if chainParams == nil {
		return nil, errors.E("unsupported network type: %s", netType)
	}

	walletsDb, err := storm.Open(filepath.Join(rootDir, walletsDbName))
	if err != nil {
		log.Errorf("Error opening wallets database: %s", err.Error())
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			return nil, errors.E("wallets database is in use by another process")
		}
		return nil, errors.E("error opening wallets database: %s", err.Error())
	}

	// load public encryption key params from wallets db
	var publicEncKeyParams []byte
	err = walletsDb.Get(walletsMetadataBucketName, walletsMetadataMasterPubKeyParamsField, &publicEncKeyParams)
	if err != nil && err != storm.ErrNotFound {
		return nil, errors.E("error reading multiwallet metadata from db: %v", err)
	}

	// unmarshal public encryption key from params
	var publicEncKey snacl.SecretKey
	if publicEncKeyParams != nil {
		err = publicEncKey.Unmarshal(publicEncKeyParams)
		if err != nil {
			return nil, errors.E("error parsing public encryption key: %v", err)
		}
	}

	// init database for saving/reading wallet objects
	err = walletsDb.Init(&Wallet{})
	if err != nil {
		log.Errorf("Error initializing wallets database: %s", err.Error())
		return nil, err
	}

	// read saved wallets info from db and initialize wallets
	query := walletsDb.Select(q.True()).OrderBy("ID")
	var wallets []*Wallet
	err = query.Find(&wallets)
	if err != nil && err != storm.ErrNotFound {
		return nil, err
	}

	// prepare the wallets loaded from db for use
	walletsMap := make(map[int]*Wallet)
	for _, wallet := range wallets {
		err = wallet.prepare(chainParams)
		if err != nil {
			return nil, err
		}
		walletsMap[wallet.ID] = wallet
	}

	mw := &MultiWallet{
		dbDriver:                  dbDriver,
		rootDir:                   rootDir,
		db:                        walletsDb,
		publicEncryptionKeyParams: publicEncKeyParams,
		publicEncryptionKey:       &publicEncKey,
		chainParams:               chainParams,
		wallets:                   walletsMap,
		syncData: &syncData{
			syncCanceled:          make(chan bool),
			syncProgressListeners: make(map[string]SyncProgressListener),
		},
		txAndBlockNotificationListeners: make(map[string]TxAndBlockNotificationListener),
	}

	mw.listenForShutdown()

	log.Infof("Loaded %d wallets", mw.LoadedWalletsCount())

	return mw, nil
}

func (mw *MultiWallet) Shutdown() {
	log.Info("Shutting down dcrlibwallet")

	// Trigger shuttingDown signal to cancel all contexts created with `shutdownContextWithCancel`.
	mw.shuttingDown <- true

	mw.CancelRescan()
	mw.CancelSync()

	for _, wallet := range mw.wallets {
		wallet.Shutdown()
	}

	if mw.db != nil {
		if err := mw.db.Close(); err != nil {
			log.Errorf("db closed with error: %v", err)
		} else {
			log.Info("db closed successfully")
		}
	}

	if logRotator != nil {
		log.Info("Shutting down log rotator")
		logRotator.Close()
	}
}

func (mw *MultiWallet) OpenWallets(pubPass []byte) error {
	if mw.syncData.activeSyncData != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	// Derive the master public encryption key for this multiwallet instance
	// using this pubPass. This also serves to verify the pubPass as the derivation
	// will only succeed if the pubPass is the correct shared public passphrase
	// for all wallets.
	err := mw.publicEncryptionKey.DeriveKey(&pubPass)
	if err != nil {
		return errors.New(ErrInvalidPassphrase)
	}

	for _, wallet := range mw.wallets {
		err = wallet.openWallet(pubPass)
		if err != nil {
			return err
		}

		go mw.listenForTransactions(wallet.ID)
	}

	return nil
}

func (mw *MultiWallet) CreateWatchOnlyWallet(walletName, publicPassphrase, extendedPublicKey string) (*Wallet, error) {
	return mw.addNewWallet(walletName, publicPassphrase, func(db storm.Node) error {
		wallet, err := mw.saveWalletPropertiesToDatabase(db, &Wallet{Name: walletName, HasDiscoveredAccounts: true})
		if err != nil {
			return err
		}

		return wallet.createWatchingOnlyWallet(publicPassphrase, extendedPublicKey)
	})
}

func (mw *MultiWallet) CreateNewWallet(publicPassphrase, privatePassphrase string, privatePassphraseType int32) (*Wallet, error) {
	return mw.addNewWallet("", publicPassphrase, func(db storm.Node) error {
		seed, err := GenerateSeed()
		if err != nil {
			return err
		}

		wallet, err := mw.saveWalletPropertiesToDatabase(db,
			&Wallet{Seed: seed, PrivatePassphraseType: privatePassphraseType, HasDiscoveredAccounts: true})
		if err != nil {
			return err
		}

		return wallet.createWallet(publicPassphrase, privatePassphrase, seed)
	})
}

func (mw *MultiWallet) RestoreWallet(seedMnemonic, publicPassphrase, privatePassphrase string, privatePassphraseType int32) (*Wallet, error) {
	return mw.addNewWallet("", publicPassphrase, func(db storm.Node) error {
		wallet, err := mw.saveWalletPropertiesToDatabase(db,
			&Wallet{PrivatePassphraseType: privatePassphraseType, HasDiscoveredAccounts: false})
		if err != nil {
			return err
		}

		return wallet.createWallet(publicPassphrase, privatePassphrase, seedMnemonic)
	})
}

func (mw *MultiWallet) addNewWallet(walletName, publicPassphrase string,
	finalizeWalletSetup func(db storm.Node) error) (*Wallet, error) {

	if mw.syncData.activeSyncData != nil {
		return nil, errors.New(ErrSyncAlreadyInProgress)
	}

	exists, err := mw.WalletNameExists(walletName)
	if err != nil {
		return nil, err
	} else if exists {
		return nil, errors.New(ErrExist)
	}

	if publicPassphrase == "" {
		publicPassphrase = w.InsecurePubPassphrase
	}

	var wallet *Wallet

	// Perform database save operations in batch transaction
	// for automatic rollback if error occurs at any point.
	err = mw.batchDbTransaction(func(db storm.Node) error {
		// Verify this wallet's pub pass against cross-wallet pub pass if this
		// is not the first wallet to be added to this multiwallet instance.
		// If this is the first wallet, save this pub pass as cross-wallet pub pass.
		if len(mw.wallets) > 0 {
			err = verifyPublicPassphrase([]byte(publicPassphrase), mw.publicEncryptionKeyParams)
			if err != nil {
				return errors.New(ErrInvalidPassphrase)
			}
		} else {
			err = mw.generateAndSavePubEncKey([]byte(publicPassphrase), db)
			if err != nil {
				return err
			}
		}

		return finalizeWalletSetup(db)
	})

	if err == nil {
		go mw.listenForTransactions(wallet.ID)
	} else if wallet != nil {
		delete(mw.wallets, wallet.ID)
		wallet = nil
	}

	// If at the end of the entire op, there's still no wallet in this
	// multiwallet instance, set the public encryption key to nil.
	if len(mw.wallets) == 0 {
		mw.publicEncryptionKeyParams = nil
		mw.publicEncryptionKey = nil
	}

	return wallet, err
}

func (mw *MultiWallet) saveWalletPropertiesToDatabase(db storm.Node, wallet *Wallet) (*Wallet, error) {
	// saving struct to update ID property with an autogenerated value
	err := db.Save(wallet)
	if err != nil {
		return nil, err
	}

	walletDataDir := filepath.Join(mw.rootDir, strconv.Itoa(wallet.ID))
	os.MkdirAll(walletDataDir, os.ModePerm) // create wallet dir

	if wallet.Name == "" {
		wallet.Name = "wallet-" + strconv.Itoa(wallet.ID) // wallet-#
	}
	wallet.DataDir = walletDataDir
	wallet.DbDriver = mw.dbDriver

	err = mw.db.Save(wallet) // update database with complete wallet information
	if err != nil {
		return nil, err
	}

	err = wallet.prepare(mw.chainParams)
	if err != nil {
		return nil, err
	}

	mw.wallets[wallet.ID] = wallet
	return wallet, nil
}

func (mw *MultiWallet) RenameWallet(walletID int, newName string) error {
	if strings.HasPrefix(newName, "wallet-") {
		return errors.E(ErrReservedWalletName)
	}

	if exists, err := mw.WalletNameExists(newName); err != nil {
		return translateError(err)
	} else if exists {
		return errors.New(ErrExist)
	}

	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrInvalid)
	}

	wallet.Name = newName
	return mw.db.Save(wallet) // update WalletName field
}

func (mw *MultiWallet) DeleteWallet(walletID int, privPass []byte) error {
	if mw.syncData.activeSyncData != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	err := wallet.deleteWallet(privPass)
	if err != nil {
		return translateError(err)
	}

	err = mw.db.DeleteStruct(wallet)
	if err != nil {
		return translateError(err)
	}

	delete(mw.wallets, walletID)

	return nil
}

func (mw *MultiWallet) WalletWithID(walletID int) *Wallet {
	if wallet, ok := mw.wallets[walletID]; ok {
		return wallet
	}
	return nil
}

func (mw *MultiWallet) VerifySeedForWallet(walletID int, seedMnemonic string) error {
	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	if wallet.Seed == seedMnemonic {
		wallet.Seed = ""
		return translateError(mw.db.Save(wallet))
	}

	return errors.New(ErrInvalid)
}

func (mw *MultiWallet) NumWalletsNeedingSeedBackup() int32 {
	var backupsNeeded int32
	for _, wallet := range mw.wallets {
		if wallet.WalletOpened() && wallet.Seed != "" {
			backupsNeeded++
		}
	}

	return backupsNeeded
}

func (mw *MultiWallet) LoadedWalletsCount() int32 {
	return int32(len(mw.wallets))
}

func (mw *MultiWallet) OpenedWalletIDsRaw() []int {
	walletIDs := make([]int, 0)
	for _, wallet := range mw.wallets {
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
	for _, wallet := range mw.wallets {
		if wallet.WalletOpened() && wallet.synced {
			syncedWallets++
		}
	}

	return syncedWallets
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

func (mw *MultiWallet) UnlockWallet(walletID int, privPass []byte) error {
	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	return wallet.UnlockWallet(privPass)
}

func (mw *MultiWallet) ChangePublicPassphrase(oldPublicPass, newPublicPass []byte) error {
	defer func() {
		for i := range oldPublicPass {
			oldPublicPass[i] = 0
		}

		for i := range newPublicPass {
			newPublicPass[i] = 0
		}
	}()

	if mw.publicEncryptionKey == nil {
		return errors.E("cross-wallet public passphrase not set, add a wallet to fix")
	}

	if len(oldPublicPass) == 0 {
		oldPublicPass = []byte(w.InsecurePubPassphrase)
	}
	if len(newPublicPass) == 0 {
		newPublicPass = []byte(w.InsecurePubPassphrase)
	}

	err := verifyPublicPassphrase(oldPublicPass, mw.publicEncryptionKeyParams)
	if err != nil {
		return errors.E(ErrInvalidPassphrase)
	}

	successfullyChangedWalletIDs := make([]int, 0)
	for walletID, wallet := range mw.wallets {
		ctx, _ := mw.contextWithShutdownCancel()
		if err = wallet.internal.ChangePublicPassphrase(ctx, oldPublicPass, newPublicPass); err != nil {
			log.Errorf("[%d] Error changing public passphrase: %v", walletID, err)
			break
		}
		successfullyChangedWalletIDs = append(successfullyChangedWalletIDs, walletID)
	}

	if err != nil {
		// Rollback changes
		for walletID := range successfullyChangedWalletIDs {
			ctx, _ := mw.contextWithShutdownCancel()
			mw.wallets[walletID].internal.ChangePublicPassphrase(ctx, newPublicPass, oldPublicPass)
		}
	} else {
		// update cross-wallet public passphrase
		err = mw.batchDbTransaction(func(db storm.Node) error {
			return mw.generateAndSavePubEncKey(newPublicPass, db)
		})
	}

	return translateError(err)
}

func (mw *MultiWallet) ChangePrivatePassphraseForWallet(walletID int, oldPrivatePassphrase, newPrivatePassphrase []byte, privatePassphraseType int32) error {
	if privatePassphraseType != PassphraseTypePin && privatePassphraseType != PassphraseTypePass {
		return errors.New(ErrInvalid)
	}

	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrInvalid)
	}

	err := wallet.changePrivatePassphrase(oldPrivatePassphrase, newPrivatePassphrase)
	if err != nil {
		return translateError(err)
	}

	wallet.PrivatePassphraseType = privatePassphraseType
	return mw.db.Save(wallet)
}
