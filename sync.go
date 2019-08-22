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
	_, k := mw.syncProgressListeners[uniqueIdentifier]
	if k {
		return errors.New(ErrListenerAlreadyExist)
	}

	mw.syncProgressListeners[uniqueIdentifier] = syncProgressListener

	// If sync is already on, notify this newly added listener of the current progress report.
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

func (mw *MultiWallet) RemoveSyncProgressListener(uniqueIdentifier string) {
	_, k := mw.syncProgressListeners[uniqueIdentifier]
	if k {
		delete(mw.syncProgressListeners, uniqueIdentifier)
	}
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

func (lw *LibWallet) SpvSync(peerAddresses string) error {
	// Unset this flag as the invocation of this method implies that any request to restart sync has been fulfilled.
	lw.syncData.restartSyncRequested = false

	loadedWallet, walletLoaded := lw.walletLoader.LoadedWallet()
	if !walletLoaded {
		return errors.New(ErrWalletNotLoaded)
	}

	// Error if the wallet is already syncing with the network.
	currentNetworkBackend, _ := loadedWallet.NetworkBackend()
	if currentNetworkBackend != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
	addrManager := addrmgr.New(lw.WalletDataDir, net.LookupIP) // TODO: be mindful of tor
	lp := p2p.NewLocalPeer(loadedWallet.ChainParams(), addr, addrManager)

	var validPeerAddresses []string
	if peerAddresses != "" {
		addresses := strings.Split(peerAddresses, ";")
		for _, address := range addresses {
			peerAddress, err := NormalizeAddress(address, lw.activeNet.Params.DefaultPort)
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
	// lw.initActiveSyncData()

	wallets := make(map[string]*wallet.Wallet)
	wallets["default"] = loadedWallet

	syncer := spv.NewSyncer(wallets, lp)
	//syncer.SetNotifications(lw.spvSyncNotificationCallbacks())
	if len(validPeerAddresses) > 0 {
		syncer.SetPersistentPeers(validPeerAddresses)
	}

	loadedWallet.SetNetworkBackend(syncer)
	lw.walletLoader.SetNetworkBackend(syncer)

	ctx, cancel := lw.contextWithShutdownCancel()
	lw.cancelSync = cancel

	// syncer.Run uses a wait group to block the thread until sync completes or an error occurs
	go func() {
		lw.syncing = true
		defer func() {
			lw.syncing = false
		}()
		err := syncer.Run(ctx)
		if err != nil {
			if err == context.Canceled {
				// lw.notifySyncCanceled()
				lw.syncCanceled <- true
			} else if err == context.DeadlineExceeded {
				// lw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("SPV synchronization deadline exceeded: %v", err))
			} else {
				// lw.notifySyncError(ErrorCodeUnexpectedError, err)
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

func (lw *LibWallet) RpcSync(networkAddress string, username string, password string, cert []byte) error {
	networkAddress, err := NormalizeAddress(networkAddress, lw.activeNet.JSONRPCClientPort)
	if err != nil {
		return errors.New(ErrInvalidAddress)
	}
	// Unset this flag as the invocation of this method implies that any request to restart sync has been fulfilled.
	lw.syncData.restartSyncRequested = false

	loadedWallet, walletLoaded := lw.walletLoader.LoadedWallet()
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
	// lw.initActiveSyncData()

	syncer := chain.NewSyncer(loadedWallet, &chain.RPCOptions{
		Address:     networkAddress,
		DefaultPort: lw.activeNet.JSONRPCClientPort,
		User:        username,
		Pass:        password,
		CA:          cert,
		Insecure:    false,
	})
	syncer.SetCallbacks(lw.generalSyncNotificationCallbacks())

	// notify sync progress listeners that connected peer count will not be reported because we're using rpc
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnPeerConnectedOrDisconnected(-1)
	}

	// syncer.Run uses a wait group to block the thread until sync completes or an error occurs
	go func() {
		lw.syncing = true
		defer func() {
			lw.syncing = false
		}()
		ctx, cancel := lw.contextWithShutdownCancel()
		lw.cancelSync = cancel
		err := syncer.Run(ctx)
		if err != nil {
			if err == context.Canceled {
				// lw.notifySyncCanceled()
				lw.syncCanceled <- true
			} else if err == context.DeadlineExceeded {
				// lw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("RPC synchronization deadline exceeded: %v", err))
			} else {
				// lw.notifySyncError(ErrorCodeUnexpectedError, err)
			}
		}
	}()

	return nil
}

func (lw *LibWallet) RestartRpcSync(networkAddress string, username string, password string, cert []byte) error {
	lw.syncData.restartSyncRequested = true
	// lw.CancelSync() // necessary to unset the network backend.
	return lw.RpcSync(networkAddress, username, password, cert)
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

			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnHeadersRescanProgress(rescanProgressReport)
			}

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
		for _, syncProgressListener := range lw.syncProgressListeners {
			syncProgressListener.OnSyncCompleted()
		}
	}()

	return nil
}

func (mw *MultiWallet) IsScanning() bool {
	return mw.syncData.rescanning
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

func (lw *LibWallet) GetConnectedPeersCount() int32 {
	return lw.connectedPeers
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
