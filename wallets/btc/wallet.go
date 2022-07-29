package btc

import (
	"context"
	"encoding/json"
	// "errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"strings"
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
	_ "github.com/btcsuite/btcwallet/walletdb/bdb" // bdb init() registers a driver
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
	"decred.org/dcrwallet/v2/errors"

	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
)

type Wallet struct {
	ID            int       `storm:"id,increment"`
	Name          string    `storm:"unique"`
	CreatedAt     time.Time `storm:"index"`
	EncryptedSeed []byte

	cl          neutrinoService
	neutrinoDB  walletdb.DB
	chainClient *chain.NeutrinoClient

	dataDir     string
	db       *storm.DB
	cancelFuncs        []context.CancelFunc

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

func NewSpvWallet(walletName string, encryptedSeed []byte, net string, log slog.Logger) (*Wallet, error) {
	chainParams, err := parseChainParams(net)
	if err != nil {
		return nil, err
	}

	return &Wallet{
		Name:          walletName,
		chainParams:   chainParams,
		CreatedAt:     time.Now(),
		EncryptedSeed: encryptedSeed,
		log:           log,
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

// createSPVWallet creates a new SPV wallet.
// func (wallet *Wallet) CreateWallet(privPass []byte, seed []byte, dbDir string) error {
// 	net := wallet.chainParams
// 	wallet.dataDir = filepath.Join(dbDir, strconv.Itoa(wallet.ID))

// 	if err := logNeutrino(wallet.dataDir); err != nil {
// 		return fmt.Errorf("error initializing btcwallet+neutrino logging: %v", err)
// 	}

// 	logDir := filepath.Join(wallet.dataDir, logDirName)
// 	err := os.MkdirAll(logDir, 0744)
// 	if err != nil {
// 		return fmt.Errorf("error creating wallet directories: %v", err)
// 	}

// 	loader := w.NewLoader(net, wallet.dataDir, true, 60*time.Second, 250)
// 	pubPass := []byte(w.InsecurePubPassphrase)

// 	_, err = loader.CreateNewWallet(pubPass, privPass, seed, walletBirthday)
// 	if err != nil {
// 		return fmt.Errorf("CreateNewWallet error: %w", err)
// 	}

// 	bailOnWallet := func() {
// 		if err := loader.UnloadWallet(); err != nil {
// 			wallet.log.Errorf("Error unloading wallet after createSPVWallet error: %v", err)
// 		}
// 	}

// 	neutrinoDBPath := filepath.Join(wallet.dataDir, neutrinoDBName)
// 	db, err := walletdb.Create("bdb", neutrinoDBPath, true, 5*time.Second)
// 	if err != nil {
// 		bailOnWallet()
// 		return fmt.Errorf("unable to create wallet db at %q: %v", neutrinoDBPath, err)
// 	}
// 	if err = db.Close(); err != nil {
// 		bailOnWallet()
// 		return fmt.Errorf("error closing newly created wallet database: %w", err)
// 	}

// 	if err := loader.UnloadWallet(); err != nil {
// 		return fmt.Errorf("error unloading wallet: %w", err)
// 	}

// 	return nil
// }

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

func (wallet *Wallet) initBtcWallet(rootDir string) error {
	// if btcWallet.BtcWallet != nil {
	// 	return nil
	// }

	btcDataDir := filepath.Join(rootDir, "btc")

	err := os.MkdirAll(btcDataDir, os.ModePerm)
	if err != nil {
		return err
	}

	walletsDB, err := storm.Open(filepath.Join(btcDataDir, "wallets.db"))
	if err != nil {
		return err
	}

	// init database for saving/reading wallet objects
	err = walletsDB.Init(&Wallet{})
	if err != nil {
		// log.Errorf("Error initializing wallets database BTC: %s", err.Error())
		return err
	}

	wallet = &Wallet{
		dataDir: filepath.Join(rootDir, "btc"),
		db:         walletsDB,
		// wallets:    make(map[int]*Wallet),
	}

	// read saved wallets info from db and initialize wallets
	query := walletsDB.Select(q.True()).OrderBy("ID")
	var wallets []*Wallet
	err = query.Find(&wallets)
	if err != nil && err != storm.ErrNotFound {
		return err
	}

	// prepare the wallets loaded from db for use
	// for _, wallet := range wallets {
	// 	// err = wallet.Prepare(btcWallet.btcDataDir, btcWallet.NetType(), log)
	// 	// if err == nil && !WalletExistsAt(wallet.DataDir()) {
	// 	// 	err = fmt.Errorf("missing wallet database file")
	// 	// }
	// 	if err != nil {
	// 		// log.Warnf("Ignored wallet load error for wallet %d (%s)", wallet.ID, wallet.Name)
	// 	} else {
	// 		btcWallet.wallets[wallet.ID] = wallet
	// 	}
	// }

	return nil
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

func (wallet *Wallet) CreateNewWallet(rootDir, walletName, privatePassphrase string) (*Wallet, error) {
	seed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
	if err != nil {
		return nil, err
	}

	// encryptedSeed, err := encryptWalletSeed([]byte(privatePassphrase), seed)
	// if err != nil {
	// 	return nil, err
	// }

	btcWallet := &Wallet{
		Name:          walletName,
		CreatedAt:     time.Now(),
		EncryptedSeed: seed,
	}

	return btcWallet.saveNewWallet( func() error {
		err := wallet.Prepare(rootDir, "testnet3", nil)
		if err != nil {
			return err
		}

		return wallet.createWallet(privatePassphrase, seed)
	})
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
	exists, err := wallet.WalletNameExists()
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

	batchDbTransaction := func(dbOp func(node storm.Node) error) (err error) {
		dbTx, err := wallet.db.Begin(true)
		if err != nil {
			return err
		}

		// Commit or rollback the transaction after f returns or panics.  Do not
		// recover from the panic to keep the original stack trace intact.
		panicked := true
		defer func() {
			if panicked || err != nil {
				dbTx.Rollback()
				return
			}

			err = dbTx.Commit()
		}()

		err = dbOp(dbTx)
		panicked = false
		return err
	}

	// Perform database save operations in batch transaction
	// for automatic rollback if error occurs at any point.
	err = batchDbTransaction(func(db storm.Node) error {
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
		// err = wallet.createWallet([]byte(password), encryptedSeed, wallet.dataDir)
		// if err != nil {
		// 	return fmt.Errorf("Create BTC wallet error: %v", err)
		// }
// 
		// return nil
	})

	if err != nil {
		return nil, err
	}

	return wallet, nil
}

func (wallet *Wallet) WalletNameExists() (bool, error) {
	if strings.HasPrefix(wallet.Name, "wallet-") {
		return false, errors.E(ErrReservedWalletName)
	}

	err := wallet.db.One("Name", wallet.Name, &Wallet{})
	if err == nil {
		return true, nil
	} else if err != storm.ErrNotFound {
		return false, err
	}

	return false, nil
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