package dcrlibwallet

import (
	"context"
	"math"
	"net"
	"strings"
	"time"

	"github.com/decred/dcrd/addrmgr"
	"github.com/decred/dcrwallet/chain/v3"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/p2p/v2"
	"github.com/decred/dcrwallet/spv/v3"
	"github.com/decred/dcrwallet/wallet/v3"
)

type syncData struct {
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

func (lw *LibWallet) initActiveSyncData() {
	headersFetchProgress := HeadersFetchProgressReport{}
	headersFetchProgress.GeneralSyncProgress = &GeneralSyncProgress{}

	addressDiscoveryProgress := AddressDiscoveryProgressReport{}
	addressDiscoveryProgress.GeneralSyncProgress = &GeneralSyncProgress{}

	headersRescanProgress := HeadersRescanProgressReport{}
	headersRescanProgress.GeneralSyncProgress = &GeneralSyncProgress{}

	var targetTimePerBlock int32
	if lw.activeNet.Name == "mainnet" {
		targetTimePerBlock = MainNetTargetTimePerBlock
	} else {
		targetTimePerBlock = TestNetTargetTimePerBlock
	}

	lw.syncData.activeSyncData = &activeSyncData{
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

func (lw *LibWallet) AddSyncProgressListener(syncProgressListener SyncProgressListener, uniqueIdentifier string) error {
	_, k := lw.syncProgressListeners[uniqueIdentifier]
	if k {
		return errors.New(ErrListenerAlreadyExist)
	}

	lw.syncProgressListeners[uniqueIdentifier] = syncProgressListener

	// If sync is already on, notify this newly added listener of the current progress report.
	if lw.syncData.syncing && lw.activeSyncData != nil {
		switch lw.activeSyncData.syncStage {
		case HeadersFetchSyncStage:
			syncProgressListener.OnHeadersFetchProgress(&lw.headersFetchProgress)
		case AddressDiscoverySyncStage:
			syncProgressListener.OnAddressDiscoveryProgress(&lw.activeSyncData.addressDiscoveryProgress)
		case HeadersRescanSyncStage:
			syncProgressListener.OnHeadersRescanProgress(&lw.activeSyncData.headersRescanProgress)
		}
	}

	return nil
}

func (lw *LibWallet) RemoveSyncProgressListener(uniqueIdentifier string) {
	_, k := lw.syncProgressListeners[uniqueIdentifier]
	if k {
		delete(lw.syncProgressListeners, uniqueIdentifier)
	}
}

func (lw *LibWallet) EnableSyncLogs() {
	lw.syncData.showLogs = true
}

func (lw *LibWallet) SyncInactiveForPeriod(totalInactiveSeconds int64) {

	if !lw.syncing || lw.activeSyncData == nil {
		log.Debug("Not accounting for inactive time, wallet is not syncing.")
		return
	}

	lw.syncData.totalInactiveSeconds += totalInactiveSeconds
	if lw.syncData.connectedPeers == 0 {
		// assume it would take another 60 seconds to reconnect to peers
		lw.syncData.totalInactiveSeconds += 60
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
	addrManager := addrmgr.New(lw.walletDataDir, net.LookupIP) // TODO: be mindful of tor
	lp := p2p.NewLocalPeer(loadedWallet.ChainParams(), addr, addrManager)

	var validPeerAddresses []string
	if peerAddresses != "" {
		addresses := strings.Split(peerAddresses, ";")
		for _, address := range addresses {
			peerAddress, err := NormalizeAddress(address, lw.activeNet.DefaultPort)
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
	lw.initActiveSyncData()

	syncer := spv.NewSyncer(loadedWallet, lp)
	syncer.SetNotifications(lw.spvSyncNotificationCallbacks())
	if len(validPeerAddresses) > 0 {
		syncer.SetPersistentPeers(validPeerAddresses)
	}

	loadedWallet.SetNetworkBackend(syncer)

	ctx, cancel := context.WithCancel(context.Background())
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
				lw.notifySyncCanceled()
				lw.syncCanceled <- true
			} else if err == context.DeadlineExceeded {
				lw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("SPV synchronization deadline exceeded: %v", err))
			} else {
				lw.notifySyncError(ErrorCodeUnexpectedError, err)
			}
		}
	}()

	return nil
}

func (lw *LibWallet) RestartSpvSync(peerAddresses string) error {
	lw.syncData.restartSyncRequested = true
	lw.CancelSync() // necessary to unset the network backend.
	return lw.SpvSync(peerAddresses)
}

func (lw *LibWallet) RpcSync(networkAddress string, username string, password string, cert []byte) error {
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

	ctx, cancel := context.WithCancel(context.Background())
	lw.cancelSync = cancel

	// init activeSyncData to be used to hold data used
	// to calculate sync estimates only during sync
	lw.initActiveSyncData()

	syncer := chain.NewSyncer(loadedWallet, &chain.RPCOptions{
		Address:     networkAddress,
		DefaultPort: rpcClientDefaultPortForNet(lw.activeNet.Name),
		User:        username,
		Pass:        password,
		CA:          cert,
		Insecure:    len(cert) == 0,
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
		err := syncer.Run(ctx)
		if err != nil {
			if err == context.Canceled {
				lw.notifySyncCanceled()
				lw.syncCanceled <- true
			} else if err == context.DeadlineExceeded {
				lw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("RPC synchronization deadline exceeded: %v", err))
			} else {
				lw.notifySyncError(ErrorCodeUnexpectedError, err)
			}
		}
	}()

	return nil
}

func (lw *LibWallet) RestartRpcSync(networkAddress string, username string, password string, cert []byte) error {
	lw.syncData.restartSyncRequested = true
	lw.CancelSync() // necessary to unset the network backend.
	return lw.RpcSync(networkAddress, username, password, cert)
}

func (lw *LibWallet) CancelSync() {
	if lw.cancelSync != nil {
		log.Info("Canceling sync. May take a while for sync to fully cancel.")

		// Cancel the context used for syncer.Run in rpcSync() or spvSync().
		lw.cancelSync()
		lw.cancelSync = nil

		// syncer.Run may not immediately return, following code blocks this function
		// and waits for the syncer.Run to return `err == context.Canceled`.
		<-lw.syncCanceled
		log.Info("Sync fully canceled.")
	}

	loadedWallet, walletLoaded := lw.walletLoader.LoadedWallet()
	if !walletLoaded {
		return
	}

	loadedWallet.SetNetworkBackend(nil)
}

func (lw *LibWallet) IsSynced() bool {
	return lw.syncData.synced
}

func (lw *LibWallet) IsSyncing() bool {
	return lw.syncData.syncing
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

		ctx := lw.shutdownContext()

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

		err = lw.reindexTransactions()
		if err != nil {
			log.Errorf("Error clearing saved transactions: %v", err)
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

func (lw *LibWallet) IsScanning() bool {
	return lw.syncData.rescanning
}

func (lw *LibWallet) GetBestBlock() int32 {
	if lw.wallet == nil {
		// This method is sometimes called after a wallet is deleted and causes crash.
		log.Error("Attempting to read best block height without a loaded wallet.")
		return 0
	}

	_, height := lw.wallet.MainChainTip(lw.shutdownContext())
	return height
}

func (lw *LibWallet) GetBestBlockTimeStamp() int64 {
	if lw.wallet == nil {
		// This method is sometimes called after a wallet is deleted and causes crash.
		log.Error("Attempting to read best block timestamp without a loaded wallet.")
		return 0
	}

	_, height := lw.wallet.MainChainTip(lw.shutdownContext())
	identifier := wallet.NewBlockIdentifierFromHeight(height)
	info, err := lw.wallet.BlockInfo(lw.shutdownContext(), identifier)
	if err != nil {
		log.Error(err)
		return 0
	}
	return info.Timestamp
}
