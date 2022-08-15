package btc

import (
	"context"
	"encoding/json"
	// "errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/gcs"
	"github.com/btcsuite/btcwallet/chain"
	// "github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	w "github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/walletdb"
	// "github.com/planetdecred/dcrlibwallet"
	"decred.org/dcrwallet/v2/errors"
	_ "github.com/btcsuite/btcwallet/walletdb/bdb" // bdb init() registers a driver
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"

	"github.com/asdine/storm"
	// "github.com/asdine/storm/q"
)

type Wallet struct {
	ID            int       `storm:"id,increment"`
	Name          string    `storm:"unique"`
	CreatedAt     time.Time `storm:"index"`
	dbDriver      string
	rootDir       string
	db            *storm.DB
	EncryptedSeed []byte

	cl          neutrinoService
	neutrinoDB  walletdb.DB
	chainClient *chain.NeutrinoClient

	dataDir     string
	cancelFuncs []context.CancelFunc

	Synced            bool


	chainParams *chaincfg.Params
	loader      *w.Loader
	log         slog.Logger
	birthday    time.Time
	// mw dcrlibwallet.MultiWallet
}

// neutrinoService is satisfied by *neutrino.ChainService.
type neutrinoService interface {
	GetBlockHash(int64) (*chainhash.Hash, error)
	BestBlock() (*headerfs.BlockStamp, error)
	Peers() []*neutrino.ServerPeer
	GetBlockHeight(hash *chainhash.Hash) (int32, error)
	GetBlockHeader(*chainhash.Hash) (*wire.BlockHeader, error)
	GetCFilter(blockHash chainhash.Hash, filterType wire.FilterType, options ...neutrino.QueryOption) (*gcs.Filter, error)
	GetBlock(blockHash chainhash.Hash, options ...neutrino.QueryOption) (*btcutil.Block, error)
	Stop() error
}

var _ neutrinoService = (*neutrino.ChainService)(nil)

var (
	walletBirthday time.Time
	loggingInited  uint32
)

const (
	neutrinoDBName = "neutrino.db"
	logDirName     = "logs"
	logFileName    = "neutrino.log"
)

func NewSpvWallet(walletName string, encryptedSeed []byte, net string) (*Wallet, error) {
	chainParams, err := parseChainParams(net)
	if err != nil {
		return nil, err
	}

	return &Wallet{
		Name:          walletName,
		chainParams:   chainParams,
		CreatedAt:     time.Now(),
		EncryptedSeed: encryptedSeed,
	}, nil
}

func parseChainParams(net string) (*chaincfg.Params, error) {
	switch net {
	case "mainnet":
		return &chaincfg.MainNetParams, nil
	case "testnet3":
		return &chaincfg.TestNet3Params, nil
	case "regtest", "regnet", "simnet":
		return &chaincfg.RegressionNetParams, nil
	}
	return nil, fmt.Errorf("unknown network ID %v", net)
}

// logWriter implements an io.Writer that outputs to a rotating log file.
type logWriter struct {
	*rotator.Rotator
}

// logNeutrino initializes logging in the neutrino + wallet packages. Logging
// only has to be initialized once, so an atomic flag is used internally to
// return early on subsequent invocations.
//
// In theory, the the rotating file logger must be Close'd at some point, but
// there are concurrency issues with that since btcd and btcwallet have
// unsupervised goroutines still running after shutdown. So we leave the rotator
// running at the risk of losing some logs.
func logNeutrino(walletDir string) error {
	if !atomic.CompareAndSwapUint32(&loggingInited, 0, 1) {
		return nil
	}

	logSpinner, err := logRotator(walletDir)
	if err != nil {
		return fmt.Errorf("error initializing log rotator: %w", err)
	}

	backendLog := btclog.NewBackend(logWriter{logSpinner})

	logger := func(name string, lvl btclog.Level) btclog.Logger {
		l := backendLog.Logger(name)
		l.SetLevel(lvl)
		return l
	}

	neutrino.UseLogger(logger("NTRNO", btclog.LevelDebug))
	w.UseLogger(logger("BTCW", btclog.LevelInfo))
	wtxmgr.UseLogger(logger("TXMGR", btclog.LevelInfo))
	chain.UseLogger(logger("CHAIN", btclog.LevelInfo))

	return nil
}

// logRotator initializes a rotating file logger.
func logRotator(netDir string) (*rotator.Rotator, error) {
	const maxLogRolls = 8
	logDir := filepath.Join(netDir, logDirName)
	if err := os.MkdirAll(logDir, 0744); err != nil {
		return nil, fmt.Errorf("error creating log directory: %w", err)
	}

	logFilename := filepath.Join(logDir, logFileName)
	return rotator.New(logFilename, 32*1024, false, maxLogRolls)
}
func (wallet *Wallet) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	// Not needed for spv wallet.
	return nil, errors.New("RawRequest not available on spv")
}

func CreateNewWallet(walletName, privatePassphrase string, privatePassphraseType int32, db *storm.DB, rootDir, dbDriver string, chainParams *chaincfg.Params) (*Wallet, error) {
	// seed := "witch collapse practice feed shame open despair"
	// encryptedSeed := []byte(seed)
	encryptedSeed, err := hdkeychain.GenerateSeed(
		hdkeychain.RecommendedSeedLen,
	)
	if err != nil {
		return nil, err
	}

	wallet := &Wallet{
		Name:          walletName,
		db:            db,
		dbDriver:      dbDriver,
		rootDir:       rootDir,
		chainParams:   chainParams,
		CreatedAt:     time.Now(),
		EncryptedSeed: encryptedSeed,
		// log:           log,
	}

	return wallet.saveNewWallet(func() error {
		err := wallet.Prepare(wallet.rootDir, "testnet3", wallet.log)
		if err != nil {
			return err
		}

		return wallet.createWallet(privatePassphrase, encryptedSeed)
	})
}

func (wallet *Wallet) RenameWallet(newName string, walledDbRef *storm.DB) error {
	if strings.HasPrefix(newName, "wallet-") {
		return errors.E(ErrReservedWalletName)
	}

	if exists, err := WalletNameExists(newName, walledDbRef); err != nil {
		return translateError(err)
	} else if exists {
		return errors.New(ErrExist)
	}

	wallet.Name = newName
	return walledDbRef.Save(wallet) // update WalletName field
}


func (wallet *Wallet) OpenWallet() error {
	pubPass := []byte(w.InsecurePubPassphrase)

	_, err := wallet.loader.OpenExistingWallet(pubPass, false)
	if err != nil {
		// log.Error(err)
		return translateError(err)
	}

	return nil
}

func (wallet *Wallet) WalletExists() (bool, error) {
	return wallet.loader.WalletExists()
}

func (wallet *Wallet) IsWatchingOnlyWallet() bool {
	if _, ok := wallet.loader.LoadedWallet(); ok {
		// return w.WatchingOnly()
		return false
	}

	return false
}

func (wallet *Wallet) WalletOpened() bool {
	return wallet.Internal() != nil
}

func (wallet *Wallet) Internal() *w.Wallet {
	lw, _ := wallet.loader.LoadedWallet()
	return lw
}

func CreateNewWatchOnlyWallet(walletName string, chainParams *chaincfg.Params) (*Wallet, error) {
	wallet := &Wallet{
		Name:          walletName,
		chainParams:   chainParams,
	}

	return wallet.saveNewWallet(func() error {
		err := wallet.Prepare(wallet.rootDir, "testnet3", wallet.log)
		if err != nil {
			return err
		}

		return wallet.createWatchingOnlyWallet()
	})
}

func (wallet *Wallet) createWatchingOnlyWallet() error {
	pubPass := []byte(w.InsecurePubPassphrase)

	_, err := wallet.loader.CreateNewWatchingOnlyWallet(pubPass, time.Now())
	if err != nil {
		// log.Error(err)
		return err
	}

	// log.Info("Created Watching Only Wallet")
	return nil
}

func (wallet *Wallet) DataDir() string {
	return wallet.dataDir
}

func (wallet *Wallet) ConnectSPVWallet(ctx context.Context, wg *sync.WaitGroup) (err error) {
	return wallet.connect(ctx, wg)
}

// connect will start the wallet and begin syncing.
func (wallet *Wallet) connect(ctx context.Context, wg *sync.WaitGroup) error {
	if err := logNeutrino(wallet.dataDir); err != nil {
		return fmt.Errorf("error initializing btcwallet+neutrino logging: %v", err)
	}

	err := wallet.startWallet()
	if err != nil {
		return err
	}

	// txNotes := wallet.wallet.txNotifications()

	// Nanny for the caches checkpoints and txBlocks caches.
	wg.Add(1)
	// go func() {
	// 	defer wg.Done()
	// 	defer wallet.stop()
	// 	defer txNotes.Done()

	// 	ticker := time.NewTicker(time.Minute * 20)
	// 	defer ticker.Stop()
	// 	expiration := time.Hour * 2
	// 	for {
	// 		select {
	// 		case <-ticker.C:
	// 			wallet.txBlocksMtx.Lock()
	// 			for txHash, entry := range wallet.txBlocks {
	// 				if time.Since(entry.lastAccess) > expiration {
	// 					delete(wallet.txBlocks, txHash)
	// 				}
	// 			}
	// 			wallet.txBlocksMtx.Unlock()

	// 			wallet.checkpointMtx.Lock()
	// 			for outPt, check := range wallet.checkpoints {
	// 				if time.Since(check.lastAccess) > expiration {
	// 					delete(wallet.checkpoints, outPt)
	// 				}
	// 			}
	// 			wallet.checkpointMtx.Unlock()

	// 		case note := <-txNotes.C:
	// 			if len(note.AttachedBlocks) > 0 {
	// 				lastBlock := note.AttachedBlocks[len(note.AttachedBlocks)-1]
	// 				syncTarget := atomic.LoadInt32(&wallet.syncTarget)

	// 				for ib := range note.AttachedBlocks {
	// 					for _, nt := range note.AttachedBlocks[ib].Transactions {
	// 						wallet.log.Debugf("Block %d contains wallet transaction %v", note.AttachedBlocks[ib].Height, nt.Hash)
	// 					}
	// 				}

	// 				if syncTarget == 0 || (lastBlock.Height < syncTarget && lastBlock.Height%10_000 != 0) {
	// 					continue
	// 				}

	// 				select {
	// 				case wallet.tipChan <- &block{
	// 					hash:   *lastBlock.Hash,
	// 					height: int64(lastBlock.Height),
	// 				}:
	// 				default:
	// 					wallet.log.Warnf("tip report channel was blocking")
	// 				}
	// 			}

	// 		case <-ctx.Done():
	// 			return
	// 		}
	// 	}
	// }()

	return nil
}

// startWallet initializes the *btcwallet.Wallet and its supporting players and
// starts syncing.
func (wallet *Wallet) startWallet() error {
	// timeout and recoverWindow arguments borrowed from btcwallet directly.
	wallet.loader = w.NewLoader(wallet.chainParams, wallet.dataDir, true, 60*time.Second, 250)

	exists, err := wallet.loader.WalletExists()
	if err != nil {
		return fmt.Errorf("error verifying wallet existence: %v", err)
	}
	if !exists {
		return errors.New("wallet not found")
	}

	wallet.log.Debug("Starting native BTC wallet...")
	btcw, err := wallet.loader.OpenExistingWallet([]byte(w.InsecurePubPassphrase), false)
	if err != nil {
		return fmt.Errorf("couldn't load wallet: %w", err)
	}

	bailOnWallet := func() {
		if err := wallet.loader.UnloadWallet(); err != nil {
			wallet.log.Errorf("Error unloading wallet: %v", err)
		}
	}

	neutrinoDBPath := filepath.Join(wallet.dataDir, neutrinoDBName)
	wallet.neutrinoDB, err = walletdb.Create("bdb", neutrinoDBPath, true, w.DefaultDBTimeout)
	if err != nil {
		bailOnWallet()
		return fmt.Errorf("unable to create wallet db at %q: %v", neutrinoDBPath, err)
	}

	bailOnWalletAndDB := func() {
		if err := wallet.neutrinoDB.Close(); err != nil {
			wallet.log.Errorf("Error closing neutrino database: %v", err)
		}
		bailOnWallet()
	}

	// Depending on the network, we add some addpeers or a connect peer. On
	// regtest, if the peers haven't been explicitly set, add the simnet harness
	// alpha node as an additional peer so we don't have to type it in. On
	// mainet and testnet3, add a known reliable persistent peer to be used in
	// addition to normal DNS seed-based peer discovery.
	var addPeers []string
	var connectPeers []string
	switch wallet.chainParams.Net {
	case wire.MainNet:
		addPeers = []string{"cfilters.ssgen.io"}
	case wire.TestNet3:
		addPeers = []string{"dex-test.ssgen.io"}
	case wire.TestNet, wire.SimNet: // plain "wire.TestNet" is regnet!
		connectPeers = []string{"localhost:20575"}
	}
	wallet.log.Debug("Starting neutrino chain service...")
	chainService, err := neutrino.NewChainService(neutrino.Config{
		DataDir:       wallet.dataDir,
		Database:      wallet.neutrinoDB,
		ChainParams:   *wallet.chainParams,
		PersistToDisk: true, // keep cfilter headers on disk for efficient rescanning
		AddPeers:      addPeers,
		ConnectPeers:  connectPeers,
		// WARNING: PublishTransaction currently uses the entire duration
		// because if an external bug, but even if the resolved, a typical
		// inv/getdata round trip is ~4 seconds, so we set this so neutrino does
		// not cancel queries too readily.
		BroadcastTimeout: 6 * time.Second,
	})
	if err != nil {
		bailOnWalletAndDB()
		return fmt.Errorf("couldn't create Neutrino ChainService: %v", err)
	}

	bailOnEverything := func() {
		if err := chainService.Stop(); err != nil {
			wallet.log.Errorf("Error closing neutrino chain service: %v", err)
		}
		bailOnWalletAndDB()
	}

	wallet.cl = chainService
	wallet.chainClient = chain.NewNeutrinoClient(wallet.chainParams, chainService)
	// wallet.wallet = &walletExtender{btcw, wallet.chainParams}

	// oldBday := btcw.Manager.Birthday()
	// wdb := btcw.Database()

	// performRescan := wallet.birthday.Before(oldBday)
	// if performRescan && !wallet.allowAutomaticRescan {
	// 	bailOnWalletAndDB()
	// 	return errors.New("cannot set earlier birthday while there are active deals")
	// }

	// if !oldBday.Equal(wallet.birthday) {
	// 	err = walletdb.Update(wdb, func(dbtx walletdb.ReadWriteTx) error {
	// 		ns := dbtx.ReadWriteBucket(wAddrMgrBkt)
	// 		return btcw.Manager.SetBirthday(ns, wallet.birthday)
	// 	})
	// 	if err != nil {
	// 		wallet.log.Errorf("Failed to reset wallet manager birthday: %v", err)
	// 		performRescan = false
	// 	}
	// }

	// if performRescan {
	// 	wallet.forceRescan()
	// }

	if err = wallet.chainClient.Start(); err != nil { // lazily starts connmgr
		bailOnEverything()
		return fmt.Errorf("couldn't start Neutrino client: %v", err)
	}

	wallet.log.Info("Synchronizing wallet with network...")
	btcw.SynchronizeRPC(wallet.chainClient)

	return nil
}

// prepare gets a wallet ready for use by opening the transactions index database
// and initializing the wallet loader which can be used subsequently to create,
// load and unload the wallet.
func (wallet *Wallet) Prepare(rootDir string, net string, log slog.Logger) (err error) {
	chainParams, err := parseChainParams(net)
	if err != nil {
		return err
	}

	wallet.chainParams = chainParams
	wallet.dataDir = filepath.Join(rootDir, strconv.Itoa(wallet.ID))
	wallet.log = log
	wallet.loader = w.NewLoader(wallet.chainParams, wallet.dataDir, true, 60*time.Second, 250)
	return nil
}

func (wallet *Wallet) NetType() string {
	return wallet.chainParams.Name
}

func (wallet *Wallet) createWallet(privatePassphrase string, seedMnemonic []byte) error {
	// log.Info("Creating Wallet")
	if len(seedMnemonic) == 0 {
		return errors.New("ErrEmptySeed")
	}

	pubPass := []byte(w.InsecurePubPassphrase)
	privPass := []byte(privatePassphrase)
	// seed, err := walletseed.DecodeUserInput(seedMnemonic)
	// if err != nil {
	// 	// log.Error(err)
	// 	return err
	// }

	_, err := wallet.loader.CreateNewWallet(pubPass, privPass, seedMnemonic, wallet.CreatedAt)
	if err != nil {
		// log.Error(err)
		return err
	}

	bailOnWallet := func() {
		if err := wallet.loader.UnloadWallet(); err != nil {
			fmt.Errorf("Error unloading wallet after createSPVWallet error: %v", err)
		}
	}

	neutrinoDBPath := filepath.Join(wallet.dataDir, neutrinoDBName)
	db, err := walletdb.Create("bdb", neutrinoDBPath, true, 5*time.Second)
	if err != nil {
		bailOnWallet()
		return fmt.Errorf("unable to create wallet db at %q: %v", neutrinoDBPath, err)
	}
	if err = db.Close(); err != nil {
		bailOnWallet()
		return fmt.Errorf("error closing newly created wallet database: %w", err)
	}

	if err := wallet.loader.UnloadWallet(); err != nil {
		return fmt.Errorf("error unloading wallet: %w", err)
	}

	// log.Info("Created Wallet")
	return nil
}

// saveNewWallet performs the following tasks using a db batch operation to ensure
// that db changes are rolled back if any of the steps below return an error.
//
// - saves the initial wallet info to btcWallet.walletsDb to get a wallet id
// - creates a data directory for the wallet using the auto-generated wallet id
// - updates the initial wallet info with name, dataDir (created above), db driver
//   and saves the updated info to btcWallet.walletsDb
// - calls the provided `setupWallet` function to perform any necessary creation,
//   restoration or linking of the just saved wallet
//
// IFF all the above operations succeed, the wallet info will be persisted to db
// and the wallet will be added to `btcWallet.wallets`.
func (wallet *Wallet) saveNewWallet(setupWallet func() error) (*Wallet, error) {
	exists, err := WalletNameExists(wallet.Name, wallet.db)
	if err != nil {
		return nil, err
	} else if exists {
		return nil, errors.New(ErrExist)
	}

	// if btcWallet.IsConnectedToDecredNetwork() {
	// 	btcWallet.CancelSync()
	// 	defer btcWallet.SpvSync()
	// }
	// Perform database save operations in batch transaction
	// for automatic rollback if error occurs at any point.
	// walletNameExists := func(walletName string) (bool, error) {
	// 	if strings.HasPrefix(walletName, "wallet-") {
	// 		return false, errors.E(ErrReservedWalletName)
	// 	}

	// 	err := wallet.db.One("Name", walletName, &Wallet{})
	// 	if err == nil {
	// 		return true, nil
	// 	} else if err != storm.ErrNotFound {
	// 		return false, err
	// 	}

	// 	return false, nil
	// }

	// exists, err := walletNameExists(wallet.Name)
	// if err != nil {
	// 	return nil, err
	// } else if exists {
	// 	return nil, errors.New(ErrExist)
	// }

	// Perform database save operations in batch transaction
	// for automatic rollback if error occurs at any point.
	err = wallet.batchDbTransaction(func(db storm.Node) error {
		// saving struct to update ID property with an auto-generated value
		err := db.Save(wallet)
		if err != nil {
			return err
		}
		walletDataDir := filepath.Join(wallet.dataDir, strconv.Itoa(wallet.ID))

		dirExists, err := fileExists(walletDataDir)
		if err != nil {
			return err
		} else if dirExists {
			_, err := backupFile(walletDataDir, 1)
			if err != nil {
				return err
			}

			// log.Infof("Undocumented file at %s moved to %s", walletDataDir, newDirName)
		}

		os.MkdirAll(walletDataDir, os.ModePerm) // create wallet dir

		if wallet.Name == "" {
			wallet.Name = "wallet-" + strconv.Itoa(wallet.ID) // wallet-#
		}
		err = db.Save(wallet) // update database with complete wallet information
		if err != nil {
			return err
		}

		return setupWallet()

	})

	if err != nil {
		return nil, err
	}

	return wallet, nil
}

func fileExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func backupFile(fileName string, suffix int) (newName string, err error) {
	newName = fileName + ".bak" + strconv.Itoa(suffix)
	exists, err := fileExists(newName)
	if err != nil {
		return "", err
	} else if exists {
		return backupFile(fileName, suffix+1)
	}

	err = moveFile(fileName, newName)
	if err != nil {
		return "", err
	}

	return newName, nil
}

func moveFile(sourcePath, destinationPath string) error {
	if exists, _ := fileExists(sourcePath); exists {
		return os.Rename(sourcePath, destinationPath)
	}
	return nil
}

func (wallet *Wallet) shutdownContextWithCancel() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	wallet.cancelFuncs = append(wallet.cancelFuncs, cancel)
	return ctx, cancel
}

func (wallet *Wallet) shutdownContext() (ctx context.Context) {
	ctx, _ = wallet.shutdownContextWithCancel()
	return
}
