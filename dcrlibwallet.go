package dcrlibwallet

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/addrmgr"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/hdkeychain"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/netparams"
	"github.com/decred/dcrwallet/p2p"
	"github.com/decred/dcrwallet/spv"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/walletseed"
	"github.com/decred/slog"
	"github.com/raedahgroup/dcrlibwallet/addresshelper"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
	"github.com/raedahgroup/dcrlibwallet/util"
)

var shutdownRequestChannel = make(chan struct{})
var shutdownSignaled = make(chan struct{})
var signals = []os.Signal{os.Interrupt, syscall.SIGTERM}

const (
	BlockValid     int = 1 << 0
	BucketTxInfo       = "TxIndexInfo"
	KeyEndBlock        = "EndBlock"
	MaxReOrgBlocks     = 6
)

type LibWallet struct {
	dataDir       string
	db            *storm.DB
	wallet        *wallet.Wallet
	rpcClient     *chain.RPCClient
	cancelSync    context.CancelFunc
	loader        *Loader
	mu            sync.Mutex
	activeNet     *netparams.Params
	syncResponses []SpvSyncResponse
	rescanning    bool
}

func NewLibWallet(homeDir string, dbDriver string, netType string) (*LibWallet, error) {
	activeNet := util.NetParams(netType)
	if activeNet == nil {
		return nil, fmt.Errorf("unsupported network type: %s", netType)
	}

	errors.Separator = ":: "
	initLogRotator(filepath.Join(homeDir, "/logs/"+netType+"/dcrlibwallet.log"))

	// init database
	stormDB, err := storm.Open(filepath.Join(homeDir, "dcrlibwallet.db"))
	if err != nil {
		log.Errorf("Open DB Error: %v", err)
		return nil, err
	}

	stormDB.Init(&Transaction{})

	// init loader
	stakeOptions := &StakeOptions{
		VotingEnabled: false,
		AddressReuse:  false,
		VotingAddress: nil,
		TicketFee:     10e8,
	}
	dataDir := filepath.Join(homeDir, netType)
	l := NewLoader(activeNet.Params, dataDir, stakeOptions, 20, false, 10e5, wallet.DefaultAccountGapLimit)
	l.SetDatabaseDriver(dbDriver)
	go shutdownListener()

	lw := &LibWallet{
		dataDir:   filepath.Join(homeDir, activeNet.Name),
		db:        stormDB,
		activeNet: activeNet,
		loader:    l,
	}

	return lw, nil
}

func LibWalletFromDb(homeDir string, dbPath string, activeNet *netparams.Params) *LibWallet {
	errors.Separator = ":: "
	initLogRotator(filepath.Join(homeDir, "/logs/"+activeNet.Name+"/dcrlibwallet.log"))

	return &LibWallet{
		dataDir:   dbPath,
		dbDriver:  DefaultDbDriver, // todo fix
		activeNet: activeNet,
	}
}

func (lw *LibWallet) SetLogLevel(loglevel string) {
	_, ok := slog.LevelFromString(loglevel)
	if ok {
		setLogLevels(loglevel)
	}
}

func NormalizeAddress(addr string, defaultPort string) (hostport string, err error) {
	// If the first SplitHostPort errors because of a missing port and not
	// for an invalid host, add the port.  If the second SplitHostPort
	// fails, then a port is not missing and the original error should be
	// returned.
	host, port, origErr := net.SplitHostPort(addr)
	if origErr == nil {
		return net.JoinHostPort(host, port), nil
	}
	addr = net.JoinHostPort(addr, defaultPort)
	_, _, err = net.SplitHostPort(addr)
	if err != nil {
		return "", origErr
	}
	return addr, nil
}

func (lw *LibWallet) UnlockWallet(privPass []byte) error {

	wallet, ok := lw.loader.LoadedWallet()
	if !ok {
		return fmt.Errorf("wallet has not been loaded")
	}

	defer func() {
		for i := range privPass {
			privPass[i] = 0
		}
	}()

	err := wallet.Unlock(privPass, nil)
	return err
}

func (lw *LibWallet) LockWallet() {
	if lw.wallet.Locked() {
		lw.wallet.Lock()
	}
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

	err := lw.wallet.ChangePrivatePassphrase(oldPass, newPass)
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

	err := lw.wallet.ChangePublicPassphrase(oldPass, newPass)
	if err != nil {
		return translateError(err)
	}
	return nil
}

func (lw *LibWallet) Shutdown(exit bool) {
	log.Info("Shutting down mobile wallet")

	if lw.rpcClient != nil {
		lw.rpcClient.Stop()
	}

	close(shutdownSignaled)

	if lw.cancelSync != nil {
		lw.cancelSync()
	}

	if logRotator != nil {
		log.Infof("Shutting down log rotator")
		logRotator.Close()
	}

	if _, loaded := lw.loader.LoadedWallet(); loaded {
		err := lw.loader.UnloadWallet()
		if err != nil {
			log.Errorf("Failed to close wallet: %v", err)
		} else {
			log.Infof("Closed wallet")
		}
	}

	if lw.db != nil {
		err := lw.db.Close()
		if err != nil {
			log.Errorf("db closed with error: %v", err)
		} else {
			log.Info("db closed successfully")
		}
	}

	if exit {
		os.Exit(0)
	}
}

func shutdownListener() {
	interruptChannel := make(chan os.Signal, 1)
	signal.Notify(interruptChannel, signals...)

	// Listen for the initial shutdown signal
	select {
	case sig := <-interruptChannel:
		log.Infof("Received signal (%s).  Shutting down...", sig)
	case <-shutdownRequestChannel:
		log.Info("Shutdown requested.  Shutting down...")
	}

	// Cancel all contexts created from withShutdownCancel.
	close(shutdownSignaled)

	// Listen for any more shutdown signals and log that shutdown has already
	// been signaled.
	for {
		select {
		case <-interruptChannel:
		case <-shutdownRequestChannel:
		}
		log.Info("Shutdown signaled.  Already shutting down...")
	}
}

func contextWithShutdownCancel(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-shutdownSignaled
		cancel()
	}()
	return ctx
}

func decodeAddress(a string, params *chaincfg.Params) (dcrutil.Address, error) {
	addr, err := dcrutil.DecodeAddress(a)
	if err != nil {
		return nil, err
	}

	if !addr.IsForNet(params) {
		return nil, fmt.Errorf("address %v is not intended for use on %v",
			a, params.Name)
	}
	return addr, nil
}

func (lw *LibWallet) WalletExists() (bool, error) {
	return lw.loader.WalletExists()
}

func (lw *LibWallet) CreateWallet(passphrase string, seedMnemonic string) error {
	log.Info("Creating Wallet")
	if len(seedMnemonic) == 0 {
		return errors.New(ErrEmptySeed)
	}
	pubPass := []byte(wallet.InsecurePubPassphrase)
	privPass := []byte(passphrase)
	seed, err := walletseed.DecodeUserInput(seedMnemonic)
	if err != nil {
		log.Error(err)
		return err
	}

	w, err := lw.loader.CreateNewWallet(pubPass, privPass, seed)
	if err != nil {
		log.Error(err)
		return err
	}
	lw.wallet = w

	log.Info("Created Wallet")
	return nil
}

func (lw *LibWallet) CloseWallet() error {
	err := lw.loader.UnloadWallet()
	return err
}

func (lw *LibWallet) GenerateSeed() (string, error) {
	seed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
	if err != nil {
		log.Error(err)
		return "", err
	}

	return walletseed.EncodeMnemonic(seed), nil
}

func (lw *LibWallet) VerifySeed(seedMnemonic string) bool {
	_, err := walletseed.DecodeUserInput(seedMnemonic)
	return err == nil
}

func (lw *LibWallet) AddSyncResponse(syncResponse SpvSyncResponse) {
	lw.syncResponses = append(lw.syncResponses, syncResponse)
}

func (lw *LibWallet) SpvSync(peerAddresses string) error {
	wallet, ok := lw.loader.LoadedWallet()
	if !ok {
		return errors.New(ErrWalletNotLoaded)
	}

	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
	amgr := addrmgr.New(lw.dataDir, net.LookupIP) // TODO: be mindful of tor
	lp := p2p.NewLocalPeer(wallet.ChainParams(), addr, amgr)

	ntfns := &spv.Notifications{
		Synced: func(sync bool) {
			lw.IndexTransactions(-1, -1)
		},
		FetchHeadersStarted: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchedHeaders(0, 0, START)
			}
		},
		FetchHeadersProgress: func(fetchedHeadersCount int32, lastHeaderTime int64) {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchedHeaders(fetchedHeadersCount, lastHeaderTime, PROGRESS)
			}
		},
		FetchHeadersFinished: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchedHeaders(0, 0, FINISH)
			}
		},
		FetchMissingCFiltersStarted: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchMissingCFilters(0, 0, START)
			}
		},
		FetchMissingCFiltersProgress: func(missingCFitlersStart, missingCFitlersEnd int32) {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchMissingCFilters(missingCFitlersStart, missingCFitlersEnd, PROGRESS)
			}
		},
		FetchMissingCFiltersFinished: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchMissingCFilters(0, 0, FINISH)
			}
		},
		DiscoverAddressesStarted: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnDiscoveredAddresses(START)
			}
		},
		DiscoverAddressesFinished: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnDiscoveredAddresses(FINISH)
			}

			if !wallet.Locked() {
				wallet.Lock()
			}
		},
		RescanStarted: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnRescan(0, START)
			}
		},
		RescanProgress: func(rescannedThrough int32) {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnRescan(rescannedThrough, PROGRESS)
			}
		},
		RescanFinished: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnRescan(0, FINISH)
			}
		},
		PeerDisconnected: func(peerCount int32, addr string) {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnPeerDisconnected(peerCount)
			}
		},
		PeerConnected: func(peerCount int32, addr string) {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnPeerConnected(peerCount)
			}
		},
	}
	var spvConnect []string
	if len(peerAddresses) > 0 {
		spvConnect = strings.Split(peerAddresses, ";")
	}
	go func() {
		syncer := spv.NewSyncer(wallet, lp)
		syncer.SetNotifications(ntfns)
		if len(spvConnect) > 0 {
			spvConnects := make([]string, len(spvConnect))
			for i := 0; i < len(spvConnect); i++ {
				spvConnect, err := NormalizeAddress(spvConnect[i], lw.activeNet.Params.DefaultPort)
				if err != nil {
					for _, syncResponse := range lw.syncResponses {
						syncResponse.OnSyncError(3, errors.E("SPV Connect addresshelper invalid: %v", err))
					}
					return
				}
				spvConnects[i] = spvConnect
			}
			syncer.SetPersistantPeers(spvConnects)
		}
		wallet.SetNetworkBackend(syncer)
		lw.loader.SetNetworkBackend(syncer)
		ctx, cancel := context.WithCancel(context.Background())
		lw.cancelSync = cancel
		err := syncer.Run(ctx)
		if err != nil {
			if err == context.Canceled {
				for _, syncResponse := range lw.syncResponses {
					syncResponse.OnSyncError(1, errors.E("SPV synchronization canceled: %v", err))
				}

				return
			} else if err == context.DeadlineExceeded {
				for _, syncResponse := range lw.syncResponses {
					syncResponse.OnSyncError(2, errors.E("SPV synchronization deadline exceeded: %v", err))
				}
				return
			}
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnSyncError(-1, err)
			}
			return
		}
	}()
	return nil
}

func (lw *LibWallet) RpcSync(networkAddress string, username string, password string, cert []byte) error {
	// Error if the wallet is already syncing with the network.
	wallet, walletLoaded := lw.loader.LoadedWallet()
	if walletLoaded {
		_, err := wallet.NetworkBackend()
		if err == nil {
			return errors.New(ErrFailedPrecondition)
		}
	}

	lw.mu.Lock()
	chainClient := lw.rpcClient
	lw.mu.Unlock()

	ctx := contextWithShutdownCancel(context.Background())
	// If the rpcClient is already set, you can just use that instead of attempting a new connection.
	if chainClient == nil {
		networkAddress, err := NormalizeAddress(networkAddress, lw.activeNet.JSONRPCClientPort)
		if err != nil {
			return errors.New(ErrInvalidAddress)
		}
		chainClient, err = chain.NewRPCClient(lw.activeNet.Params, networkAddress, username,
			password, cert, len(cert) == 0)
		if err != nil {
			return translateError(err)
		}

		err = chainClient.Start(ctx, false)
		if err != nil {
			if err == rpcclient.ErrInvalidAuth {
				return errors.New(ErrInvalid)
			}
			if errors.Match(errors.E(context.Canceled), err) {
				return errors.New(ErrContextCanceled)
			}
			return errors.New(ErrUnavailable)
		}
		lw.mu.Lock()
		lw.rpcClient = chainClient
		lw.mu.Unlock()
	}

	n := chain.BackendFromRPCClient(chainClient.Client)
	lw.loader.SetNetworkBackend(n)
	wallet.SetNetworkBackend(n)

	// Disassociate the RPC client from all subsystems until reconnection
	// occurs.
	defer lw.wallet.SetNetworkBackend(nil)
	defer lw.loader.SetNetworkBackend(nil)
	defer lw.loader.StopTicketPurchase()

	ntfns := &chain.Notifications{
		Synced: func(sync bool) {
			lw.IndexTransactions(-1, -1)
		},
		FetchMissingCFiltersStarted: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchMissingCFilters(0, 0, START)
			}
		},
		FetchMissingCFiltersProgress: func(missingCFitlersStart, missingCFitlersEnd int32) {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchMissingCFilters(missingCFitlersStart, missingCFitlersEnd, PROGRESS)
			}
		},
		FetchMissingCFiltersFinished: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchMissingCFilters(0, 0, FINISH)
			}
		},
		FetchHeadersStarted: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchedHeaders(0, 0, START)
			}
		},
		FetchHeadersProgress: func(fetchedHeadersCount int32, lastHeaderTime int64) {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchedHeaders(fetchedHeadersCount, lastHeaderTime, PROGRESS)
			}
		},
		FetchHeadersFinished: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnFetchedHeaders(0, 0, FINISH)
			}
		},
		DiscoverAddressesStarted: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnDiscoveredAddresses(START)
			}
		},
		DiscoverAddressesFinished: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnDiscoveredAddresses(FINISH)
			}

			if !wallet.Locked() {
				wallet.Lock()
			}
		},
		RescanStarted: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnRescan(0, START)
			}
		},
		RescanProgress: func(rescannedThrough int32) {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnRescan(rescannedThrough, PROGRESS)
			}
		},
		RescanFinished: func() {
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnRescan(0, FINISH)
			}
		},
	}
	syncer := chain.NewRPCSyncer(wallet, chainClient)
	syncer.SetNotifications(ntfns)

	go func() {
		// Run wallet synchronization until it is cancelled or errors.  If the
		// context was cancelled, return immediately instead of trying to
		// reconnect.
		err := syncer.Run(ctx, true)
		if err != nil {
			if err == context.Canceled {
				for _, syncResponse := range lw.syncResponses {
					syncResponse.OnSyncError(1, errors.E("SPV synchronization canceled: %v", err))
				}

				return
			} else if err == context.DeadlineExceeded {
				for _, syncResponse := range lw.syncResponses {
					syncResponse.OnSyncError(2, errors.E("SPV synchronization deadline exceeded: %v", err))
				}

				return
			}
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnSyncError(-1, err)
			}
		}
	}()

	return nil
}

func (lw *LibWallet) DropSpvConnection() {
	if lw.cancelSync != nil {
		lw.cancelSync()
	}

	for _, syncResponse := range lw.syncResponses {
		syncResponse.OnSynced(false)
	}
}

func (lw *LibWallet) OpenWallet(pubPass []byte) error {

	w, err := lw.loader.OpenExistingWallet(pubPass)
	if err != nil {
		log.Error(err)
		return translateError(err)
	}
	lw.wallet = w
	return nil
}

func (lw *LibWallet) WalletOpened() bool {
	return lw.wallet != nil
}

func (lw *LibWallet) RescanBlocks() error {
	netBackend, err := lw.wallet.NetworkBackend()
	if err != nil {
		return errors.E(ErrNotConnected)
	}

	if lw.rescanning {
		return errors.E(ErrInvalid)
	}

	go func() {
		defer func() {
			lw.rescanning = false
		}()
		lw.rescanning = true
		progress := make(chan wallet.RescanProgress, 1)
		ctx := contextWithShutdownCancel(context.Background())
		var totalHeight int32
		go lw.wallet.RescanProgressFromHeight(ctx, netBackend, 0, progress)
		for p := range progress {
			if p.Err != nil {
				log.Error(p.Err)

				return
			}
			totalHeight += p.ScannedThrough
			for _, response := range lw.syncResponses {
				response.OnRescan(p.ScannedThrough, PROGRESS)
			}
		}
		select {
		case <-ctx.Done():
			for _, response := range lw.syncResponses {
				response.OnRescan(totalHeight, PROGRESS)
			}
		default:
			for _, response := range lw.syncResponses {
				response.OnRescan(totalHeight, FINISH)
			}
		}
	}()

	return nil
}

func (lw *LibWallet) GetBestBlock() int32 {
	_, height := lw.wallet.MainChainTip()
	return height
}

func (lw *LibWallet) GetBestBlockTimeStamp() int64 {
	_, height := lw.wallet.MainChainTip()
	identifier := wallet.NewBlockIdentifierFromHeight(height)
	info, err := lw.wallet.BlockInfo(identifier)
	if err != nil {
		log.Error(err)
		return 0
	}
	return info.Timestamp
}

func (lw *LibWallet) IndexTransactions(beginHeight int32, endHeight int32) error {
	ctx := contextWithShutdownCancel(context.Background())

	var totalIndex int32
	rangeFn := func(block *wallet.Block) (bool, error) {
		for _, transaction := range block.Transactions {

			var blockHash *chainhash.Hash
			if block.Header != nil {
				hash := block.Header.BlockHash()
				blockHash = &hash
			} else {
				blockHash = nil
			}

			tx, err := lw.parseTxSummary(&transaction, blockHash)
			if err != nil {
				return false, err
			}

			err = lw.replaceTxIfExist(tx)
			if err != nil {
				log.Errorf("Index tx replace tx err :%v", err)
				return false, err
			}

			totalIndex++
			for _, syncResponse := range lw.syncResponses {
				syncResponse.OnIndexTransactions(totalIndex)
			}
		}

		if block.Header != nil {
			err := lw.db.Set(BucketTxInfo, KeyEndBlock, &endHeight)
			if err != nil {
				log.Errorf("Set tx index end block height error: ", err)
				return false, err
			}

			log.Infof("Transaction index caught up to %d", endHeight)
		}

		select {
		case <-ctx.Done():
			return true, ctx.Err()
		default:
			return false, nil
		}
	}

	if beginHeight == -1 {
		var previousEndBlock int32
		err := lw.db.Get(BucketTxInfo, KeyEndBlock, &previousEndBlock)
		if err != nil && err != storm.ErrNotFound {
			log.Errorf("Get not found :%v", err)
			return err
		}

		beginHeight = previousEndBlock
		beginHeight -= MaxReOrgBlocks

		if beginHeight < 0 {
			beginHeight = 0
		}
	}

	if beginHeight > endHeight {
		endHeight = lw.GetBestBlock()
	}

	startBlock := wallet.NewBlockIdentifierFromHeight(beginHeight)
	endBlock := wallet.NewBlockIdentifierFromHeight(endHeight)

	defer func() {
		for _, syncResponse := range lw.syncResponses {
			syncResponse.OnSynced(true)
		}
		count, err := lw.db.Count(&Transaction{})
		if err != nil {
			log.Errorf("Count Error :%v", err)
			return
		}
		log.Infof("Transaction index finished at %d, %d transaction(s) indexed in total", endHeight, count)
	}()

	log.Infof("Indexing transactions start height: %d, end height: %d", beginHeight, endHeight)
	return lw.wallet.GetTransactions(rangeFn, startBlock, endBlock)
}

func (lw *LibWallet) TransactionNotification(listener TransactionListener) {
	go func() {
		n := lw.wallet.NtfnServer.TransactionNotifications()
		defer n.Done()
		for {
			v := <-n.C
			for _, transaction := range v.UnminedTransactions {
				tempTransaction, err := lw.parseTxSummary(&transaction, nil)
				if err != nil {
					log.Errorf("Error ntfn parse tx: %v", err)
					return
				}

				err = lw.replaceTxIfExist(tempTransaction)
				if err != nil {
					log.Errorf("Tx ntfn replace tx err: %v", err)
				}

				fmt.Println("New Transaction")
				result, err := json.Marshal(tempTransaction)
				if err != nil {
					log.Error(err)
				} else {
					listener.OnTransaction(string(result))
				}
			}
			for _, block := range v.AttachedBlocks {
				listener.OnBlockAttached(int32(block.Header.Height), block.Header.Timestamp.UnixNano())
				for _, transaction := range block.Transactions {
					tempTransaction, err := lw.parseTxSummary(&transaction, nil)
					if err != nil {
						log.Errorf("Error ntfn parse tx: %v", err)
						return
					}

					err = lw.replaceTxIfExist(tempTransaction)
					if err != nil {
						log.Errorf("Incoming block replace tx error :%v", err)
						return
					}
					listener.OnTransactionConfirmed(transaction.Hash.String(), int32(block.Header.Height))
				}
			}
		}
	}()
}

func (lw *LibWallet) GetTransaction(txHash []byte) (string, error) {
	transaction, err := lw.GetTransactionRaw(txHash)
	if err != nil {
		return "", err
	}

	result, err := json.Marshal(transaction)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

func (lw *LibWallet) GetTransactionRaw(txHash []byte) (*Transaction, error) {
	hash, err := chainhash.NewHash(txHash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	txSummary, confirmations, blockHash, err := lw.wallet.TransactionSummary(hash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	tx, err := lw.parseTxSummary(txSummary, blockHash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	tx.Confirmations = confirmations
	return tx, nil
}

func (lw *LibWallet) GetTransactions(limit int32) (string, error) {
	var transactions []Transaction

	query := lw.db.Select().OrderBy("Timestamp").Reverse()
	if limit > 0 {
		query = query.Limit(int(limit))
	}

	err := query.Find(&transactions)
	if err != nil {
		return "", nil
	}

	jsonEncodedTransactions, err := json.Marshal(&transactions)
	if err != nil {
		return "", err
	}

	return string(jsonEncodedTransactions), nil
}

func (lw *LibWallet) replaceTxIfExist(tx *Transaction) error {
	var oldTx Transaction
	err := lw.db.One("Hash", tx.Hash, &oldTx)
	if err != nil {
		if err != storm.ErrNotFound {
			log.Errorf("Find old tx error: %v", err)
			return err
		}
	} else {
		err = lw.db.DeleteStruct(&oldTx)
		if err != nil {
			log.Errorf("Delete old tx error: %v", err)
			return err
		}
	}

	err = lw.db.Save(tx)
	if err != nil {
		log.Errorf("Save transaction error :%v", err)
		return err
	}

	return nil
}

func (lw *LibWallet) parseTxSummary(txSummary *wallet.TransactionSummary, blockHash *chainhash.Hash) (*Transaction, error) {
	var inputTotal int64
	var outputTotal int64

	credits := make([]*TransactionCredit, len(txSummary.MyOutputs))
	for index, credit := range txSummary.MyOutputs {
		outputTotal += int64(credit.Amount)
		credits[index] = &TransactionCredit{
			Index:    int32(credit.Index),
			Account:  int32(credit.Account),
			Internal: credit.Internal,
			Amount:   int64(credit.Amount),
			Address:  credit.Address.String()}
	}

	debits := make([]*TransactionDebit, len(txSummary.MyInputs))
	for index, debit := range txSummary.MyInputs {
		inputTotal += int64(debit.PreviousAmount)
		debits[index] = &TransactionDebit{
			Index:           int32(debit.Index),
			PreviousAccount: int32(debit.PreviousAccount),
			PreviousAmount:  int64(debit.PreviousAmount),
			AccountName:     lw.AccountName(debit.PreviousAccount)}
	}

	amount, direction := txhelper.TransactionAmountAndDirection(inputTotal, outputTotal, int64(txSummary.Fee))

	var height int32 = -1
	if txSummary != nil {
		blockIdentifier := wallet.NewBlockIdentifierFromHash(blockHash)
		blockInfo, err := lw.wallet.BlockInfo(blockIdentifier)
		if err != nil {
			log.Error(err)
		} else {
			height = blockInfo.Height
		}
	}

	return &Transaction{
		Fee:       int64(txSummary.Fee),
		Hash:      txSummary.Hash.String(),
		Raw:       fmt.Sprintf("%02x", txSummary.Transaction[:]),
		Timestamp: txSummary.Timestamp,
		Type:      txhelper.TransactionType(txSummary.Type),
		Credits:   credits,
		Amount:    amount,
		BlockHeight:    height,
		Direction: direction,
		Debits:    debits}, nil
}

func (lw *LibWallet) GetTransactionsInBlockRange(ctx context.Context, startBlock, endBlock *wallet.BlockIdentifier) (
	transactions []*Transaction, err error) {

	rangeFn := func(block *wallet.Block) (bool, error) {
		for _, transaction := range block.Transactions {
			var inputAmounts int64
			var outputAmounts int64
			var amount int64
			tempCredits := make([]*TransactionCredit, len(transaction.MyOutputs))
			for index, credit := range transaction.MyOutputs {
				outputAmounts += int64(credit.Amount)
				tempCredits[index] = &TransactionCredit{
					Index:    int32(credit.Index),
					Account:  int32(credit.Account),
					Internal: credit.Internal,
					Amount:   int64(credit.Amount),
					Address:  credit.Address.String()}
			}
			tempDebits := make([]*TransactionDebit, len(transaction.MyInputs))
			for index, debit := range transaction.MyInputs {
				inputAmounts += int64(debit.PreviousAmount)
				tempDebits[index] = &TransactionDebit{
					Index:           int32(debit.Index),
					PreviousAccount: int32(debit.PreviousAccount),
					PreviousAmount:  int64(debit.PreviousAmount),
					AccountName:     lw.AccountName(debit.PreviousAccount)}
			}

			var direction txhelper.TransactionDirection
			if transaction.Type == wallet.TransactionTypeRegular {
				amountDifference := outputAmounts - inputAmounts
				if amountDifference < 0 && (float64(transaction.Fee) == math.Abs(float64(amountDifference))) {
					//Transfered
					direction = txhelper.TransactionDirectionTransferred
					amount = int64(transaction.Fee)
				} else if amountDifference > 0 {
					//Received
					direction = txhelper.TransactionDirectionReceived
					for _, credit := range transaction.MyOutputs {
						amount += int64(credit.Amount)
					}
				} else {
					//Sent
					direction = txhelper.TransactionDirectionSent
					for _, debit := range transaction.MyInputs {
						amount += int64(debit.PreviousAmount)
					}
					for _, credit := range transaction.MyOutputs {
						amount -= int64(credit.Amount)
					}
					amount -= int64(transaction.Fee)
				}
			}
			var height int32 = -1
			if block.Header != nil {
				height = int32(block.Header.Height)
			}
			tempTransaction := &Transaction{
				Fee:         int64(transaction.Fee),
				Hash:        transaction.Hash.String(),
				Raw:         fmt.Sprintf("%02x", transaction.Transaction[:]),
				Timestamp:   transaction.Timestamp,
				Type:        txhelper.TransactionType(transaction.Type),
				Credits:     tempCredits,
				Amount:      amount,
				BlockHeight: height,
				Direction:   direction,
				Debits:      tempDebits}
			transactions = append(transactions, tempTransaction)
		}
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		default:
			return false, nil
		}
	}

	err = lw.wallet.GetTransactions(rangeFn, startBlock, endBlock)
	return
}

func (lw *LibWallet) DecodeTransaction(txHash []byte) (string, error) {
	hash, err := chainhash.NewHash(txHash)
	if err != nil {
		log.Error(err)
		return "", err
	}
	txSummary, _, _, err := lw.wallet.TransactionSummary(hash)
	if err != nil {
		log.Error(err)
		return "", err
	}

	tx, err := txhelper.DecodeTransaction(hash, txSummary.Transaction, lw.activeNet.Params, lw.AddressInfo)
	if err != nil {
		log.Error(err)
		return "", err
	}

	result, _ := json.Marshal(tx)
	return string(result), nil
}

func (lw *LibWallet) UnspentOutputs(account uint32, requiredConfirmations int32, targetAmount int64) ([]*UnspentOutput, error) {
	policy := wallet.OutputSelectionPolicy{
		Account:               account,
		RequiredConfirmations: requiredConfirmations,
	}
	inputDetail, err := lw.wallet.SelectInputs(dcrutil.Amount(targetAmount), policy)
	// Do not return errors to caller when there was insufficient spendable
	// outputs available for the target amount.
	if err != nil && !errors.Is(errors.InsufficientBalance, err) {
		return nil, err
	}

	unspentOutputs := make([]*UnspentOutput, len(inputDetail.Inputs))

	for i, input := range inputDetail.Inputs {
		outputInfo, err := lw.wallet.OutputInfo(&input.PreviousOutPoint)
		if err != nil {
			return nil, err
		}

		// unique key to identify utxo
		outputKey := fmt.Sprintf("%s:%d", input.PreviousOutPoint.Hash, input.PreviousOutPoint.Index)

		unspentOutputs[i] = &UnspentOutput{
			TransactionHash: input.PreviousOutPoint.Hash[:],
			OutputIndex:     input.PreviousOutPoint.Index,
			OutputKey:    	 outputKey,
			Tree:            int32(input.PreviousOutPoint.Tree),
			Amount:          int64(outputInfo.Amount),
			PkScript:        inputDetail.Scripts[i],
			ReceiveTime:     outputInfo.Received.Unix(),
			FromCoinbase:    outputInfo.FromCoinbase,
		}
	}

	return unspentOutputs, nil
}

func (lw *LibWallet) SpendableForAccount(account int32, requiredConfirmations int32) (int64, error) {
	bals, err := lw.wallet.CalculateAccountBalance(uint32(account), requiredConfirmations)
	if err != nil {
		log.Error(err)
		return 0, err
	}
	return int64(bals.Spendable), nil
}

func (lw *LibWallet) ConstructTransaction(destAddr string, amount int64, srcAccount int32, requiredConfirmations int32, sendAll bool) (*UnsignedTransaction, error) {
	// output destination
	pkScript, err := addresshelper.PkScript(destAddr)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	version := txscript.DefaultScriptVersion

	// pay output
	outputs := make([]*wire.TxOut, 0)
	var algo wallet.OutputSelectionAlgorithm = wallet.OutputSelectionAlgorithmAll
	var changeSource txauthor.ChangeSource
	if !sendAll {
		algo = wallet.OutputSelectionAlgorithmDefault
		output := &wire.TxOut{
			Value:    amount,
			Version:  version,
			PkScript: pkScript,
		}
		outputs = append(outputs, output)
	} else {
		changeSource, err = txhelper.MakeTxChangeSource(destAddr)
		if err != nil {
			log.Error(err)
			return nil, err
		}
	}
	feePerKb := txrules.DefaultRelayFeePerKb

	// create tx
	tx, err := lw.wallet.NewUnsignedTransaction(outputs, feePerKb, uint32(srcAccount),
		requiredConfirmations, algo, changeSource)
	if err != nil {
		log.Error(err)
		return nil, translateError(err)
	}

	if tx.ChangeIndex >= 0 {
		tx.RandomizeChangePosition()
	}

	var txBuf bytes.Buffer
	txBuf.Grow(tx.Tx.SerializeSize())
	err = tx.Tx.Serialize(&txBuf)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	var totalOutput dcrutil.Amount
	for _, txOut := range outputs {
		totalOutput += dcrutil.Amount(txOut.Value)
	}

	return &UnsignedTransaction{
		UnsignedTransaction:       txBuf.Bytes(),
		TotalOutputAmount:         int64(totalOutput),
		TotalPreviousOutputAmount: int64(tx.TotalInput),
		EstimatedSignedSize:       tx.EstimatedSignedSerializeSize,
		ChangeIndex:               tx.ChangeIndex,
	}, nil
}

func (lw *LibWallet) SendTransaction(privPass []byte, destAddr string, amount int64, srcAccount int32, requiredConfs int32, sendAll bool) ([]byte, error) {
	// output destination
	pkScript, err := addresshelper.PkScript(destAddr)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	// pay output
	outputs := make([]*wire.TxOut, 0)
	var algo wallet.OutputSelectionAlgorithm = wallet.OutputSelectionAlgorithmAll
	var changeSource txauthor.ChangeSource
	if !sendAll {
		algo = wallet.OutputSelectionAlgorithmDefault
		output := &wire.TxOut{
			Value:    amount,
			Version:  txscript.DefaultScriptVersion,
			PkScript: pkScript,
		}
		outputs = append(outputs, output)
	} else {
		changeSource, err = txhelper.MakeTxChangeSource(destAddr)
		if err != nil {
			log.Error(err)
			return nil, err
		}
	}

	// create tx
	unsignedTx, err := lw.wallet.NewUnsignedTransaction(outputs, txrules.DefaultRelayFeePerKb, uint32(srcAccount),
		requiredConfs, algo, changeSource)
	if err != nil {
		log.Error(err)
		return nil, translateError(err)
	}

	if unsignedTx.ChangeIndex >= 0 {
		unsignedTx.RandomizeChangePosition()
	}

	var txBuf bytes.Buffer
	txBuf.Grow(unsignedTx.Tx.SerializeSize())
	err = unsignedTx.Tx.Serialize(&txBuf)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	return lw.SignAndPublishTransaction(txBuf.Bytes(), privPass)
}

func (lw *LibWallet) BulkSendTransaction(privPass []byte, destinations []txhelper.TransactionDestination, srcAccount int32, requiredConfs int32) ([]byte, error) {
	nOutputs := len(destinations)
	var maxAmountRecipientAddress string
	for _, destination := range destinations {
		if destination.SendMax && maxAmountRecipientAddress != "" {
			return nil, fmt.Errorf("cannot send max amount to multiple recipients")
		} else {
			maxAmountRecipientAddress = destination.Address
			nOutputs--
		}
	}

	// create transaction outputs for all destination addresses and amounts, excluding destination for send max
	outputs := make([]*wire.TxOut, nOutputs)
	for i, destination := range destinations {
		if !destination.SendMax {
			output, err := txhelper.MakeTxOutput(destination)
			if err != nil {
				log.Error(err)
				return nil, err
			}

			outputs[i] = output
		}
	}

	var algo wallet.OutputSelectionAlgorithm
	var changeSource txauthor.ChangeSource
	var err error

	// if no max amount recipient, use default utxo selection algorithm and nil change source
	// so that a change source to the sending account is automatically created
	// otherwise, create a change source for the max amount recipient so that the remaining change from the tx is sent to the max amount recipient
	if maxAmountRecipientAddress != "" {
		algo = wallet.OutputSelectionAlgorithmAll
		changeSource, err = txhelper.MakeTxChangeSource(maxAmountRecipientAddress)
		if err != nil {
			log.Error(err)
			return nil, err
		}
	} else {
		algo = wallet.OutputSelectionAlgorithmDefault
	}

	unsignedTx, err := lw.wallet.NewUnsignedTransaction(outputs, txrules.DefaultRelayFeePerKb, uint32(srcAccount), requiredConfs, algo, changeSource)
	if err != nil {
		log.Error(err)
		return nil, translateError(err)
	}

	if unsignedTx.ChangeIndex >= 0 {
		unsignedTx.RandomizeChangePosition()
	}

	var txBuf bytes.Buffer
	txBuf.Grow(unsignedTx.Tx.SerializeSize())
	err = unsignedTx.Tx.Serialize(&txBuf)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	return lw.SignAndPublishTransaction(txBuf.Bytes(), privPass)
}

func (lw *LibWallet) SendFromCustomInputs(sourceAccount uint32, requiredConfirmations int32, utxoKeys []string,
	txDestinations []txhelper.TransactionDestination, changeDestinations []txhelper.TransactionDestination, privPass []byte) (string, error) {

	// fetch all utxos in account to extract details for the utxos selected by user
	// use targetAmount = 0 to fetch ALL utxos in account
	unspentOutputs, err := lw.UnspentOutputs(sourceAccount, requiredConfirmations, 0)
	if err != nil {
		return "", err
	}

	// loop through unspentOutputs to find user selected utxos
	inputs := make([]*wire.TxIn, 0, len(utxoKeys))
	var totalInputAmount int64
	for _, utxo := range unspentOutputs {
		useUtxo := false
		for _, key := range utxoKeys {
			if utxo.OutputKey == key {
				useUtxo = true
			}
		}
		if !useUtxo {
			continue
		}

		// this is a reverse conversion and should not throw an error
		// this []byte was originally converted from chainhash.Hash using chainhash.Hash[:]
		txHash, _ := chainhash.NewHash(utxo.TransactionHash)

		outpoint := wire.NewOutPoint(txHash, utxo.OutputIndex, int8(utxo.Tree))
		input := wire.NewTxIn(outpoint, int64(utxo.Amount), nil)
		inputs = append(inputs, input)
		totalInputAmount += input.ValueIn

		if len(inputs) == len(utxoKeys) {
			break
		}
	}

	// check if there's a max amount recipient, and not more than 1 such recipient
	nOutputs := len(txDestinations)
	var maxAmountRecipientAddress string
	for _, destination := range txDestinations {
		if destination.SendMax && maxAmountRecipientAddress != "" {
			return "", fmt.Errorf("cannot send max amount to multiple recipients")
		} else {
			maxAmountRecipientAddress = destination.Address
			nOutputs--
		}
	}

	// create transaction outputs for all destination addresses and amounts, excluding destination for send max
	outputs := make([]*wire.TxOut, nOutputs)
	var totalSendAmount int64
	for i, destination := range txDestinations {
		if !destination.SendMax {
			output, err := txhelper.MakeTxOutput(destination)
			if err != nil {
				log.Error(err)
				return "", err
			}

			outputs[i] = output
			totalSendAmount += output.Value
		}
	}

	if maxAmountRecipientAddress != "" {
		// use as change address
		changeAddresses := []string{maxAmountRecipientAddress}
		changeAmount, err := txhelper.EstimateChangeWithOutputs(len(inputs), totalInputAmount, outputs, totalSendAmount, changeAddresses)
		if err != nil {
			return "", err
		}

		changeDestinations = []txhelper.TransactionDestination{
			{
				Address: maxAmountRecipientAddress,
				Amount: dcrutil.Amount(changeAmount).ToCoin(),
			},
		}
	}

	unsignedTx, err := txhelper.NewUnsignedTx(inputs, outputs, changeDestinations)
	if err != nil {
		return "", err
	}

	// serialize unsigned tx
	var txBuf bytes.Buffer
	txBuf.Grow(unsignedTx.SerializeSize())
	err = unsignedTx.Serialize(&txBuf)
	if err != nil {
		return "", fmt.Errorf("error serializing transaction: %s", err.Error())
	}

	txHash, err := lw.SignAndPublishTransaction(txBuf.Bytes(), privPass)
	if err != nil {
		return "", err
	}

	transactionHash, err := chainhash.NewHash(txHash)
	if err != nil {
		return "", fmt.Errorf("error parsing successful transaction hash: %s", err.Error())
	}

	return transactionHash.String(), nil
}

func (lw *LibWallet) SignAndPublishTransaction(serializedTx, privPass []byte) ([]byte, error) {
	n, err := lw.wallet.NetworkBackend()
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer func() {
		for i := range privPass {
			privPass[i] = 0
		}
	}()

	var tx wire.MsgTx
	err = tx.Deserialize(bytes.NewReader(serializedTx))
	if err != nil {
		log.Error(err)
		//Bytes do not represent a valid raw transaction
		return nil, err
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{}
	}()

	err = lw.wallet.Unlock(privPass, lock)
	if err != nil {
		log.Error(err)
		return nil, errors.New(ErrInvalidPassphrase)
	}

	var additionalPkScripts map[wire.OutPoint][]byte

	invalidSigs, err := lw.wallet.SignTransaction(&tx, txscript.SigHashAll, additionalPkScripts, nil, nil)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	invalidInputIndexes := make([]uint32, len(invalidSigs))
	for i, e := range invalidSigs {
		invalidInputIndexes[i] = e.InputIndex
	}

	var serializedTransaction bytes.Buffer
	serializedTransaction.Grow(tx.SerializeSize())
	err = tx.Serialize(&serializedTransaction)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	var msgTx wire.MsgTx
	err = msgTx.Deserialize(bytes.NewReader(serializedTransaction.Bytes()))
	if err != nil {
		//Invalid tx
		log.Error(err)
		return nil, err
	}

	txHash, err := lw.wallet.PublishTransaction(&msgTx, serializedTransaction.Bytes(), n)
	if err != nil {
		return nil, translateError(err)
	}
	return txHash[:], nil
}

func (lw *LibWallet) PublishUnminedTransactions() error {
	netBackend, err := lw.wallet.NetworkBackend()
	if err != nil {
		return errors.New(ErrNotConnected)
	}
	ctx := contextWithShutdownCancel(context.Background())
	err = lw.wallet.PublishUnminedTransactions(ctx, netBackend)
	return err
}

func (lw *LibWallet) GetAccounts(requiredConfirmations int32) (string, error) {
	accountsResponse, err := lw.GetAccountsRaw(requiredConfirmations)
	if err != nil {
		return "", nil
	}

	result, _ := json.Marshal(accountsResponse)
	return string(result), nil
}

func (lw *LibWallet) GetAccountsRaw(requiredConfirmations int32) (*Accounts, error) {
	resp, err := lw.wallet.Accounts()
	if err != nil {
		return nil, err
	}
	accounts := make([]*Account, len(resp.Accounts))
	for i, account := range resp.Accounts {
		balance, err := lw.GetAccountBalance(account.AccountNumber, requiredConfirmations)
		if err != nil {
			return nil, err
		}

		accounts[i] = &Account{
			Number:           int32(account.AccountNumber),
			Name:             account.AccountName,
			TotalBalance:     int64(account.TotalBalance),
			Balance:          balance,
			ExternalKeyCount: int32(account.LastUsedExternalIndex + 20),
			InternalKeyCount: int32(account.LastUsedInternalIndex + 20),
			ImportedKeyCount: int32(account.ImportedKeyCount),
		}
	}

	return &Accounts{
		Count:              len(resp.Accounts),
		CurrentBlockHash:   resp.CurrentBlockHash[:],
		CurrentBlockHeight: resp.CurrentBlockHeight,
		Acc:                accounts,
		ErrorOccurred:      false,
	}, nil
}

func (lw *LibWallet) GetAccountBalance(accountNumber uint32, requiredConfirmations int32) (*Balance, error) {
	balance, err := lw.wallet.CalculateAccountBalance(accountNumber, requiredConfirmations)
	if err != nil {
		return nil, err
	}

	return &Balance{
		Total:                   int64(balance.Total),
		Spendable:               int64(balance.Spendable),
		ImmatureReward:          int64(balance.ImmatureCoinbaseRewards),
		ImmatureStakeGeneration: int64(balance.ImmatureStakeGeneration),
		LockedByTickets:         int64(balance.LockedByTickets),
		VotingAuthority:         int64(balance.VotingAuthority),
		UnConfirmed:             int64(balance.Unconfirmed),
	}, nil
}

func (lw *LibWallet) NextAccount(accountName string, privPass []byte) error {
	_, err := lw.NextAccountRaw(accountName, privPass)
	if err != nil {
		log.Error(err)
		return err
	}
	return nil
}

func (lw *LibWallet) NextAccountRaw(accountName string, privPass []byte) (uint32, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		for i := range privPass {
			privPass[i] = 0
		}
		lock <- time.Time{} // send matters, not the value
	}()
	err := lw.wallet.Unlock(privPass, lock)
	if err != nil {
		log.Error(err)
		return 0, errors.New(ErrInvalidPassphrase)
	}

	return lw.wallet.NextAccount(accountName)
}

func (lw *LibWallet) RenameAccount(accountNumber int32, newName string) error {
	err := lw.wallet.RenameAccount(uint32(accountNumber), newName)
	return err
}

func (lw *LibWallet) HaveAddress(address string) bool {
	addr, err := addresshelper.DecodeForNetwork(address, lw.activeNet.Params)
	if err != nil {
		return false
	}
	have, err := lw.wallet.HaveAddress(addr)
	if err != nil {
		return false
	}

	return have
}

func (lw *LibWallet) IsAddressValid(address string) bool {
	_, err := addresshelper.DecodeForNetwork(address, lw.activeNet.Params)
	return err == nil
}

func (lw *LibWallet) AccountName(accountNumber uint32) string {
	name, err := lw.AccountNameRaw(accountNumber)
	if err != nil {
		log.Error(err)
		return "Account not found"
	}
	return name
}

func (lw *LibWallet) AccountNameRaw(accountNumber uint32) (string, error) {
	return lw.wallet.AccountName(accountNumber)
}

func (lw *LibWallet) AccountNumber(accountName string) (uint32, error) {
	return lw.wallet.AccountNumber(accountName)
}

func (lw *LibWallet) AccountOfAddress(address string) string {
	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		return err.Error()
	}
	info, _ := lw.wallet.AddressInfo(addr)
	return lw.AccountName(info.Account())
}

func (lw *LibWallet) AddressInfo(address string) (*txhelper.AddressInfo, error) {
	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	addressInfo := &txhelper.AddressInfo{
		Address: address,
	}

	info, _ := lw.wallet.AddressInfo(addr)
	if info != nil {
		addressInfo.IsMine = true
		addressInfo.AccountNumber = info.Account()
		addressInfo.AccountName = lw.AccountName(info.Account())
	}

	return addressInfo, nil
}

func (lw *LibWallet) CurrentAddress(account int32) (string, error) {
	addr, err := lw.wallet.CurrentAddress(uint32(account))
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.EncodeAddress(), nil
}

func (lw *LibWallet) NextAddress(account int32) (string, error) {
	var callOpts []wallet.NextAddressCallOption
	callOpts = append(callOpts, wallet.WithGapPolicyWrap())

	addr, err := lw.wallet.NewExternalAddress(uint32(account), callOpts...)
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.EncodeAddress(), nil
}

// StakeInfo returns information about wallet stakes, tickets and their statuses.
func (lw *LibWallet) StakeInfo() (*wallet.StakeInfoData, error) {
	if n, err := lw.wallet.NetworkBackend(); err == nil {
		chainClient, _ := chain.RPCClientFromBackend(n)
		if chainClient != nil {
			return lw.wallet.StakeInfoPrecise(chainClient)
		}
	}

	return lw.wallet.StakeInfo()
}

func (lw *LibWallet) GetTickets(req *GetTicketsRequest) (<-chan *GetTicketsResponse, <-chan error, error) {
	var startBlock, endBlock *wallet.BlockIdentifier
	if req.StartingBlockHash != nil && req.StartingBlockHeight != 0 {
		return nil, nil, fmt.Errorf("starting block hash and height may not be specified simultaneously")
	} else if req.StartingBlockHash != nil {
		startBlockHash, err := chainhash.NewHash(req.StartingBlockHash)
		if err != nil {
			return nil, nil, err
		}
		startBlock = wallet.NewBlockIdentifierFromHash(startBlockHash)
	} else if req.StartingBlockHeight != 0 {
		startBlock = wallet.NewBlockIdentifierFromHeight(req.StartingBlockHeight)
	}

	if req.EndingBlockHash != nil && req.EndingBlockHeight != 0 {
		return nil, nil, fmt.Errorf("ending block hash and height may not be specified simultaneously")
	} else if req.EndingBlockHash != nil {
		endBlockHash, err := chainhash.NewHash(req.EndingBlockHash)
		if err != nil {
			return nil, nil, err
		}
		endBlock = wallet.NewBlockIdentifierFromHash(endBlockHash)
	} else if req.EndingBlockHeight != 0 {
		endBlock = wallet.NewBlockIdentifierFromHeight(req.EndingBlockHeight)
	}

	targetTicketCount := int(req.TargetTicketCount)
	if targetTicketCount < 0 {
		return nil, nil, fmt.Errorf("target ticket count may not be negative")
	}

	ticketCount := 0

	ch := make(chan *GetTicketsResponse)
	errCh := make(chan error)

	rangeFn := func(tickets []*wallet.TicketSummary, block *wire.BlockHeader) (bool, error) {
		resp := &GetTicketsResponse{
			Block: marshalGetTicketBlockDetails(block),
		}

		for _, t := range tickets {
			resp.TicketStatus = marshalTicketDetails(t)
			resp.Ticket = t
			ch <- resp
		}
		ticketCount += len(tickets)

		return ((targetTicketCount > 0) && (ticketCount >= targetTicketCount)), nil
	}

	go func() {
		var chainClient *rpcclient.Client
		if n, err := lw.wallet.NetworkBackend(); err == nil {
			client, err := chain.RPCClientFromBackend(n)
			if err == nil {
				chainClient = client
			}
		}
		if chainClient != nil {
			errCh <- lw.wallet.GetTicketsPrecise(rangeFn, chainClient, startBlock, endBlock)
		} else {
			errCh <- lw.wallet.GetTickets(rangeFn, startBlock, endBlock)
		}
		close(errCh)
		close(ch)
	}()

	return ch, errCh, nil
}

// TicketPrice returns the price of a ticket for the next block, also known as the stake difficulty.
// May be incorrect if blockchain sync is ongoing or if blockchain is not up-to-date.
func (lw *LibWallet) TicketPrice(ctx context.Context) (*TicketPriceResponse, error) {
	sdiff, err := lw.wallet.NextStakeDifficulty()
	if err == nil {
		_, tipHeight := lw.wallet.MainChainTip()
		resp := &TicketPriceResponse{
			TicketPrice: int64(sdiff),
			Height:      tipHeight,
		}
		return resp, nil
	}

	n, err := lw.wallet.NetworkBackend()
	if err != nil {
		return nil, err
	}
	chainClient, err := chain.RPCClientFromBackend(n)
	if err != nil {
		return nil, translateError(err)
	}

	ticketPrice, err := n.StakeDifficulty(ctx)
	if err != nil {
		return nil, translateError(err)
	}
	_, blockHeight, err := chainClient.GetBestBlock()
	if err != nil {
		return nil, translateError(err)
	}

	return &TicketPriceResponse{
		TicketPrice: int64(ticketPrice),
		Height:      int32(blockHeight),
	}, nil
}

// PurchaseTickets purchases tickets from the wallet. Returns a slice of hashes for tickets purchased
func (lw *LibWallet) PurchaseTickets(ctx context.Context, request *PurchaseTicketsRequest) ([]string, error) {
	var err error

	// Unmarshall the received data and prepare it as input for the ticket purchase request.
	ticketPriceResponse, err := lw.TicketPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not determine ticket price, %s", err.Error())
	}

	// Use current ticket price as spend limit
	spendLimit := dcrutil.Amount(ticketPriceResponse.TicketPrice)

	minConf := int32(request.RequiredConfirmations)
	params := lw.activeNet.Params

	var ticketAddr dcrutil.Address
	if request.TicketAddress != "" {
		ticketAddr, err = addresshelper.DecodeForNetwork(request.TicketAddress, params)
		if err != nil {
			return nil, err
		}
	}

	var poolAddr dcrutil.Address
	if request.PoolAddress != "" {
		poolAddr, err = addresshelper.DecodeForNetwork(request.PoolAddress, params)
		if err != nil {
			return nil, err
		}
	}

	if request.PoolFees > 0 {
		if !txrules.ValidPoolFeeRate(request.PoolFees) {
			return nil, errors.New("Invalid pool fees percentage")
		}
	}

	if request.PoolFees > 0 && poolAddr == nil {
		return nil, errors.New("Pool fees set but no pool addresshelper given")
	}

	if request.PoolFees <= 0 && poolAddr != nil {
		return nil, errors.New("Pool fees negative or unset but pool addresshelper given")
	}

	numTickets := int(request.NumTickets)
	if numTickets < 1 {
		return nil, errors.New("Zero or negative number of tickets given")
	}

	expiry := int32(request.Expiry)
	txFee := dcrutil.Amount(request.TxFee)
	ticketFee := lw.wallet.TicketFeeIncrement()

	// Set the ticket fee if specified
	if request.TicketFee > 0 {
		ticketFee = dcrutil.Amount(request.TicketFee)
	}

	if txFee < 0 || ticketFee < 0 {
		return nil, errors.New("Negative fees per KB given")
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = lw.wallet.Unlock(request.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	purchasedTickets, err := lw.wallet.PurchaseTickets(0, spendLimit, minConf, ticketAddr, request.Account, numTickets, poolAddr,
		request.PoolFees, expiry, txFee, ticketFee)
	if err != nil {
		return nil, fmt.Errorf("unable to purchase tickets: %s", err.Error())
	}

	hashes := make([]string, len(purchasedTickets))
	for i, hash := range purchasedTickets {
		hashes[i] = hash.String()
	}

	return hashes, nil
}

func (lw *LibWallet) SignMessage(passphrase []byte, address string, message string) ([]byte, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{}
	}()
	err := lw.wallet.Unlock(passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	addr, err := addresshelper.DecodeForNetwork(address, lw.activeNet.Params)
	if err != nil {
		return nil, translateError(err)
	}

	var sig []byte
	switch a := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
	case *dcrutil.AddressPubKeyHash:
		if a.DSA(a.Net()) != dcrec.STEcdsaSecp256k1 {
			return nil, errors.New(ErrInvalidAddress)
		}
	default:
		return nil, errors.New(ErrInvalidAddress)
	}

	sig, err = lw.wallet.SignMessage(message, addr)
	if err != nil {
		return nil, translateError(err)
	}

	return sig, nil
}

func (lw *LibWallet) VerifyMessage(address string, message string, signatureBase64 string) (bool, error) {
	var valid bool

	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		return false, translateError(err)
	}

	signature, err := DecodeBase64(signatureBase64)
	if err != nil {
		return false, err
	}

	// Addresses must have an associated secp256k1 private key and therefore
	// must be P2PK or P2PKH (P2SH is not allowed).
	switch a := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
	case *dcrutil.AddressPubKeyHash:
		if a.DSA(a.Net()) != dcrec.STEcdsaSecp256k1 {
			return false, errors.New(ErrInvalidAddress)
		}
	default:
		return false, errors.New(ErrInvalidAddress)
	}

	valid, err = wallet.VerifyMessage(message, addr, signature)
	if err != nil {
		return false, translateError(err)
	}

	return valid, nil
}

func (lw *LibWallet) CallJSONRPC(method string, args string, address string, username string, password string, caCert string) (string, error) {
	arguments := strings.Split(args, ",")
	params := make([]interface{}, 0)
	for _, arg := range arguments {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		params = append(params, strings.TrimSpace(arg))
	}
	// Attempt to create the appropriate command using the arguments
	// provided by the user.
	cmd, err := dcrjson.NewCmd(method, params...)
	if err != nil {
		// Show the error along with its error code when it's a
		// dcrjson.Error as it reallistcally will always be since the
		// NewCmd function is only supposed to return errors of that
		// type.
		if jerr, ok := err.(dcrjson.Error); ok {
			log.Errorf("%s command: %v (code: %s)\n",
				method, err, jerr.Code)
			return "", err
		}
		// The error is not a dcrjson.Error and this really should not
		// happen.  Nevertheless, fallback to just showing the error
		// if it should happen due to a bug in the package.
		log.Errorf("%s command: %v\n", method, err)
		return "", err
	}

	// Marshal the command into a JSON-RPC byte slice in preparation for
	// sending it to the RPC server.
	marshalledJSON, err := dcrjson.MarshalCmd("1.0", 1, cmd)
	if err != nil {
		log.Error(err)
		return "", err
	}

	// Send the JSON-RPC request to the server using the user-specified
	// connection configuration.
	result, err := sendPostRequest(marshalledJSON, address, username, password, caCert)
	if err != nil {
		log.Error(err)
		return "", err
	}

	// Choose how to display the result based on its type.
	strResult := string(result)
	if strings.HasPrefix(strResult, "{") || strings.HasPrefix(strResult, "[") {
		var dst bytes.Buffer
		if err := json.Indent(&dst, result, "", "  "); err != nil {
			log.Errorf("Failed to format result: %v", err)
			return "", err
		}
		fmt.Println(dst.String())
		return dst.String(), nil

	} else if strings.HasPrefix(strResult, `"`) {
		var str string
		if err := json.Unmarshal(result, &str); err != nil {
			log.Errorf("Failed to unmarshal result: %v", err)
			return "", err
		}
		fmt.Println(str)
		return str, nil

	} else if strResult != "null" {
		fmt.Println(strResult)
		return strResult, nil
	}
	return "", nil
}

func translateError(err error) error {
	if err, ok := err.(*errors.Error); ok {
		switch err.Kind {
		case errors.InsufficientBalance:
			return errors.New(ErrInsufficientBalance)
		case errors.NotExist:
			return errors.New(ErrNotExist)
		case errors.Passphrase:
			return errors.New(ErrInvalidPassphrase)
		case errors.NoPeers:
			return errors.New(ErrNoPeers)
		}
	}
	return err
}

func DecodeBase64(base64Text string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(base64Text)
	if err != nil {
		return nil, err
	}

	return b, nil
}
