package dcrlibwallet

import (
	"context"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/addrmgr"
	chain "github.com/decred/dcrwallet/chain/v3"
	"github.com/decred/dcrwallet/errors"
	p2p "github.com/decred/dcrwallet/p2p/v2"
	wallet "github.com/decred/dcrwallet/wallet/v3"
	spv "github.com/raedahgroup/dcrlibwallet/spv"
)

type syncData struct {
	mu        sync.Mutex
	rpcClient *chain.Syncer

	syncProgressListeners map[string]SyncProgressListener
	showLogs              bool

	synced       bool
	syncing      bool
	cancelSync   context.CancelFunc
	syncCanceled chan bool

	// Flag to notify syncCanceled callback if the sync was canceled so as to be restarted.
	restartSyncRequested bool

	rescanning bool

	connectedPeers int32

	*activeSyncData
}

type activeSyncData struct {
	targetTimePerBlock int32

	syncStage int32

	headersFetchProgress     HeadersFetchProgressReport
	addressDiscoveryProgress AddressDiscoveryProgressReport
	headersRescanProgress    HeadersRescanProgressReport

	beginFetchTimeStamp      int64
	totalFetchedHeadersCount int32
	startHeaderHeight        int32
	headersFetchTimeSpent    int64

	addressDiscoveryStartTime int64
	totalDiscoveryTimeSpent   int64

	addressDiscoveryCompleted chan bool

	rescanStartTime int64

	totalInactiveSeconds int64
}

type SyncErrorCode int32

const (
	ErrorCodeUnexpectedError SyncErrorCode = iota
	ErrorCodeDeadlineExceeded
)

const (
	InvalidSyncStage          = -1
	HeadersFetchSyncStage     = 0
	AddressDiscoverySyncStage = 1
	HeadersRescanSyncStage    = 2
)

func (mw *MultiWallet) initActiveSyncData() {
	headersFetchProgress := HeadersFetchProgressReport{}
	headersFetchProgress.GeneralSyncProgress = &GeneralSyncProgress{}

	addressDiscoveryProgress := AddressDiscoveryProgressReport{}
	addressDiscoveryProgress.GeneralSyncProgress = &GeneralSyncProgress{}

	headersRescanProgress := HeadersRescanProgressReport{}
	headersRescanProgress.GeneralSyncProgress = &GeneralSyncProgress{}

	var targetTimePerBlock int32
	if mw.activeNet.Name == "mainnet" {
		targetTimePerBlock = MainNetTargetTimePerBlock
	} else {
		targetTimePerBlock = TestNetTargetTimePerBlock
	}

	mw.syncData.activeSyncData = &activeSyncData{
		targetTimePerBlock: targetTimePerBlock,

		syncStage: InvalidSyncStage,

		headersFetchProgress:     headersFetchProgress,
		addressDiscoveryProgress: addressDiscoveryProgress,
		headersRescanProgress:    headersRescanProgress,

		beginFetchTimeStamp:     -1,
		headersFetchTimeSpent:   -1,
		totalDiscoveryTimeSpent: -1,
	}
}

func (mw *MultiWallet) AddSyncProgressListener(syncProgressListener SyncProgressListener, uniqueIdentifier string) error {
	_, ok := mw.syncProgressListeners[uniqueIdentifier]
	if ok {
		return errors.New(ErrListenerAlreadyExist)
	}

	mw.syncProgressListeners[uniqueIdentifier] = syncProgressListener

	// If sync is already on, notify this newly added listener of the current progress report.
	return mw.PublishLastSyncProgress(uniqueIdentifier)
}

func (mw *MultiWallet) RemoveSyncProgressListener(uniqueIdentifier string) {
	_, k := mw.syncProgressListeners[uniqueIdentifier]
	if k {
		delete(mw.syncProgressListeners, uniqueIdentifier)
	}
}

func (mw *MultiWallet) PublishLastSyncProgress(uniqueIdentifier string) error {
	syncProgressListener, ok := mw.syncProgressListeners[uniqueIdentifier]
	if !ok {
		return errors.New(ErrInvalid)
	}

	if mw.syncData.syncing && mw.activeSyncData != nil {
		switch mw.activeSyncData.syncStage {
		case HeadersFetchSyncStage:
			syncProgressListener.OnHeadersFetchProgress(&mw.headersFetchProgress)
		case AddressDiscoverySyncStage:
			syncProgressListener.OnAddressDiscoveryProgress(&mw.activeSyncData.addressDiscoveryProgress)
		case HeadersRescanSyncStage:
			syncProgressListener.OnHeadersRescanProgress(&mw.activeSyncData.headersRescanProgress)
		}
	}

	return nil
}

func (mw *MultiWallet) EnableSyncLogs() {
	mw.syncData.showLogs = true
}

func (mw *MultiWallet) SyncInactiveForPeriod(totalInactiveSeconds int64) {

	if !mw.syncing || mw.activeSyncData == nil {
		log.Debug("Not accounting for inactive time, wallet is not syncing.")
		return
	}

	mw.syncData.totalInactiveSeconds += totalInactiveSeconds
	if mw.syncData.connectedPeers == 0 {
		// assume it would take another 60 seconds to reconnect to peers
		mw.syncData.totalInactiveSeconds += 60
	}
}

func (mw *MultiWallet) SpvSync(peerAddresses string) error {
	// Unset this flag as the invocation of this method implies that any request to restart sync has been fulfilled.
	mw.syncData.restartSyncRequested = false

	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
	addrManager := addrmgr.New(mw.rootDir, net.LookupIP) // TODO: be mindful of tor
	lp := p2p.NewLocalPeer(mw.activeNet.Params, addr, addrManager)

	var validPeerAddresses []string
	if peerAddresses != "" {
		addresses := strings.Split(peerAddresses, ";")
		for _, address := range addresses {
			peerAddress, err := NormalizeAddress(address, mw.activeNet.Params.DefaultPort)
			if err != nil {
				log.Errorf("SPV peer address invalid: %v", err)
			} else {
				validPeerAddresses = append(validPeerAddresses, peerAddress)
			}
		}

		if len(validPeerAddresses) == 0 {
			return errors.New(ErrInvalidPeers)
		}
	}

	// init activeSyncData to be used to hold data used
	// to calculate sync estimates only during sync
	mw.initActiveSyncData()

	wallets := make(map[int]*wallet.Wallet)
	for id, wallet := range mw.wallets {
		if wallet.WalletOpened() {
			wallets[id] = wallet.wallet
		}
	}

	syncer := spv.NewSyncer(wallets, mw.activeNet.Params, lp)
	syncer.SetNotifications(mw.spvSyncNotificationCallbacks())
	if len(validPeerAddresses) > 0 {
		syncer.SetPersistentPeers(validPeerAddresses)
	}

	mw.setNetworkBackend(syncer)

	ctx, cancel := context.WithCancel(context.Background())
	mw.cancelSync = cancel

	// syncer.Run uses a wait group to block the thread until sync completes or an error occurs
	go func() {
		mw.syncing = true
		defer func() {
			mw.syncing = false
		}()

		for _, listener := range mw.syncData.syncProgressListeners {
			listener.OnSyncStarted()
		}
		err := syncer.Run(ctx)
		if err != nil {
			if err == context.Canceled {
				mw.notifySyncCanceled()
				mw.syncCanceled <- true
			} else if err == context.DeadlineExceeded {
				mw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("SPV synchronization deadline exceeded: %v", err))
			} else {
				mw.notifySyncError(ErrorCodeUnexpectedError, err)
			}
		}
	}()
	return nil
}

func (mw *MultiWallet) RestartSpvSync(peerAddresses string) error {
	mw.syncData.restartSyncRequested = true
	mw.CancelSync() // necessary to unset the network backend.
	return mw.SpvSync(peerAddresses)
}

func (mw *MultiWallet) RpcSync(networkAddress string, username string, password string, cert []byte) error {
	networkAddress, err := NormalizeAddress(networkAddress, mw.activeNet.JSONRPCClientPort)
	if err != nil {
		return errors.New(ErrInvalidAddress)
	}
	// Unset this flag as the invocation of this method implies that any request to restart sync has been fulfilled.
	mw.syncData.restartSyncRequested = false

	defaultWallet := mw.wallets[0] // TODO
	loadedWallet, walletLoaded := defaultWallet.walletLoader.LoadedWallet()
	if !walletLoaded {
		return errors.New(ErrWalletNotLoaded)
	}

	// Error if the wallet is already syncing with the network.
	currentNetworkBackend, _ := loadedWallet.NetworkBackend()
	if currentNetworkBackend != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	// init activeSyncData to be used to hold data used
	// to calculate sync estimates only during sync
	mw.initActiveSyncData()

	syncer := chain.NewSyncer(loadedWallet, &chain.RPCOptions{
		Address:     networkAddress,
		DefaultPort: mw.activeNet.JSONRPCClientPort,
		User:        username,
		Pass:        password,
		CA:          cert,
		Insecure:    false,
	})
	// syncer.SetCallbacks(mw.generalSyncNotificationCallbacks())

	// notify sync progress listeners that connected peer count will not be reported because we're using rpc
	for _, syncProgressListener := range mw.syncProgressListeners {
		syncProgressListener.OnPeerConnectedOrDisconnected(-1)
	}

	// syncer.Run uses a wait group to block the thread until sync completes or an error occurs
	go func() {
		mw.syncing = true
		defer func() {
			mw.syncing = false
		}()
		ctx, cancel := mw.contextWithShutdownCancel()
		mw.cancelSync = cancel
		err := syncer.Run(ctx)
		if err != nil {
			if err == context.Canceled {
				mw.notifySyncCanceled()
				mw.syncCanceled <- true
			} else if err == context.DeadlineExceeded {
				mw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("RPC synchronization deadline exceeded: %v", err))
			} else {
				mw.notifySyncError(ErrorCodeUnexpectedError, err)
			}
		}
	}()

	return nil
}

func (mw *MultiWallet) RestartRpcSync(networkAddress string, username string, password string, cert []byte) error {
	mw.syncData.restartSyncRequested = true
	// mw.CancelSync() // necessary to unset the network backend.
	return mw.RpcSync(networkAddress, username, password, cert)
}

func (mw *MultiWallet) CancelSync() {
	if mw.cancelSync != nil {
		log.Info("Canceling sync. May take a while for sync to fully cancel.")

		// Cancel the context used for syncer.Run in rpcSync() or spvSync().
		mw.cancelSync()
		mw.cancelSync = nil

		// syncer.Run may not immediately return, following code blocks this function
		// and waits for the syncer.Run to return `err == context.Canceled`.
		<-mw.syncCanceled
		log.Info("Sync fully canceled.")
	}

	for _, libWallet := range mw.wallets {
		loadedWallet, walletLoaded := libWallet.walletLoader.LoadedWallet()
		if !walletLoaded {
			continue
		}

		libWallet.walletLoader.SetNetworkBackend(nil)
		loadedWallet.SetNetworkBackend(nil)
	}
}

func (mw *MultiWallet) IsSynced() bool {
	return mw.syncData.synced
}

func (mw *MultiWallet) IsSyncing() bool {
	return mw.syncData.syncing
}

func (mw *MultiWallet) ConnectedPeers() int32 {
	return mw.syncData.connectedPeers
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
		ctx, _ := lw.contextWithShutdownCancel()

		progress := make(chan wallet.RescanProgress, 1)
		go lw.wallet.RescanProgressFromHeight(ctx, netBackend, 0, progress)

		rescanStartTime := time.Now().Unix()

		for p := range progress {
			if p.Err != nil {
				log.Error(p.Err)
				return
			}

			rescanProgressReport := &HeadersRescanProgressReport{
				CurrentRescanHeight: p.ScannedThrough,
				TotalHeadersToScan:  lw.GetBestBlock(),
			}

			elapsedRescanTime := time.Now().Unix() - rescanStartTime
			rescanRate := float64(p.ScannedThrough) / float64(rescanProgressReport.TotalHeadersToScan)

			rescanProgressReport.RescanProgress = int32(math.Round(rescanRate * 100))
			estimatedTotalRescanTime := int64(math.Round(float64(elapsedRescanTime) / rescanRate))
			rescanProgressReport.RescanTimeRemaining = estimatedTotalRescanTime - elapsedRescanTime

			// for _, syncProgressListener := range mw.syncProgressListeners {
			// 	syncProgressListener.OnHeadersRescanProgress(rescanProgressReport)
			// }

			select {
			case <-ctx.Done():
				log.Info("Rescan cancelled through context")
				return
			default:
				continue
			}
		}

		// Trigger sync completed callback.
		// todo: probably best to have a dedicated rescan listener
		// with callbacks for rescanStarted, rescanCompleted, rescanError and rescanCancel
		// for _, syncProgressListener := range lw.syncProgressListeners {
		// 	syncProgressListener.OnSyncCompleted()
		// }
	}()

	return nil
}

func (mw *MultiWallet) IsScanning() bool {
	return mw.syncData.rescanning
}

func (mw *MultiWallet) GetBestBlock() *BlockInfo {
	var bestBlock int32 = -1
	var blockInfo *BlockInfo
	for _, w := range mw.wallets {
		if !w.WalletOpened() {
			continue
		}

		walletBestBLock := w.GetBestBlock()
		if walletBestBLock > bestBlock || bestBlock == -1 {
			bestBlock = walletBestBLock
			blockInfo = &BlockInfo{Height: bestBlock, Timestamp: w.GetBestBlockTimeStamp()}
		}
	}

	return blockInfo
}

func (mw *MultiWallet) GetLowestBlock() *BlockInfo {
	var lowestBlock int32 = -1
	var blockInfo *BlockInfo
	for _, w := range mw.wallets {
		if !w.WalletOpened() {
			continue
		}
		walletBestBLock := w.GetBestBlock()
		if walletBestBLock < lowestBlock || lowestBlock == -1 {
			lowestBlock = walletBestBLock
			blockInfo = &BlockInfo{Height: lowestBlock, Timestamp: w.GetBestBlockTimeStamp()}
		}
	}

	return blockInfo
}

func (lw *LibWallet) GetBestBlock() int32 {
	if lw.wallet == nil {
		// This method is sometimes called after a wallet is deleted and causes crash.
		log.Error("Attempting to read best block height without a loaded wallet.")
		return 0
	}

	_, height := lw.wallet.MainChainTip()
	return height
}

func (lw *LibWallet) GetBestBlockTimeStamp() int64 {
	if lw.wallet == nil {
		// This method is sometimes called after a wallet is deleted and causes crash.
		log.Error("Attempting to read best block timestamp without a loaded wallet.")
		return 0
	}

	_, height := lw.wallet.MainChainTip()
	identifier := wallet.NewBlockIdentifierFromHeight(height)
	info, err := lw.wallet.BlockInfo(identifier)
	if err != nil {
		log.Error(err)
		return 0
	}
	return info.Timestamp
}

func (mw *MultiWallet) GetConnectedPeersCount() int32 {
	return mw.connectedPeers
}

func (mw *MultiWallet) GetLowestBlockTimestamp() int64 {
	var timestamp int64 = -1
	for _, w := range mw.wallets {
		bestBlockTimestamp := w.GetBestBlockTimeStamp()
		if bestBlockTimestamp < timestamp || timestamp == -1 {
			timestamp = bestBlockTimestamp
		}
	}
	return timestamp
}
