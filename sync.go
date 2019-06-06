package dcrlibwallet

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/decred/dcrd/addrmgr"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/p2p"
	"github.com/decred/dcrwallet/spv"
	"github.com/decred/dcrwallet/wallet"
	"math"
	"time"
)

type syncData struct {
	mu        sync.Mutex
	rpcClient *chain.RPCClient

	syncProgressListeners map[string]SyncProgressListener
	showLogs              bool

	syncing    bool
	cancelSync context.CancelFunc

	rescanning   bool
	cancelRescan context.CancelFunc

	connectedPeers int32
	peersWG        sync.WaitGroup

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
	lw.initActiveSyncData()

	syncer := spv.NewSyncer(loadedWallet, lp)
	syncer.SetNotifications(lw.spvSyncNotificationCallbacks())
	if len(validPeerAddresses) > 0 {
		syncer.SetPersistantPeers(validPeerAddresses)
	}

	loadedWallet.SetNetworkBackend(syncer)
	lw.walletLoader.SetNetworkBackend(syncer)

	ctx, cancel := contextWithShutdownCancel(context.Background())
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
			} else if err == context.DeadlineExceeded {
				lw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("SPV synchronization deadline exceeded: %v", err))
			} else {
				lw.notifySyncError(ErrorCodeUnexpectedError, err)
			}
		}
	}()

	return nil
}

func (lw *LibWallet) RpcSync(networkAddress string, username string, password string, cert []byte) error {
	loadedWallet, walletLoaded := lw.walletLoader.LoadedWallet()
	if !walletLoaded {
		return errors.New(ErrWalletNotLoaded)
	}

	// Error if the wallet is already syncing with the network.
	currentNetworkBackend, _ := loadedWallet.NetworkBackend()
	if currentNetworkBackend != nil {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	ctx, cancel := contextWithShutdownCancel(context.Background())
	lw.cancelSync = cancel

	chainClient, err := lw.connectToRpcClient(ctx, networkAddress, username, password, cert)
	if err != nil {
		return err
	}

	// init activeSyncData to be used to hold data used
	// to calculate sync estimates only during sync
	lw.initActiveSyncData()

	syncer := chain.NewRPCSyncer(loadedWallet, chainClient)
	syncer.SetNotifications(lw.generalSyncNotificationCallbacks())

	networkBackend := chain.BackendFromRPCClient(chainClient.Client)
	lw.walletLoader.SetNetworkBackend(networkBackend)
	loadedWallet.SetNetworkBackend(networkBackend)

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
		err := syncer.Run(ctx, true)
		if err != nil {
			if err == context.Canceled {
				lw.notifySyncCanceled()
			} else if err == context.DeadlineExceeded {
				lw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("RPC synchronization deadline exceeded: %v", err))
			} else {
				lw.notifySyncError(ErrorCodeUnexpectedError, err)
			}
		}
	}()

	return nil
}

func (lw *LibWallet) connectToRpcClient(ctx context.Context, networkAddress string, username string, password string,
	cert []byte) (chainClient *chain.RPCClient, err error) {

	lw.mu.Lock()
	chainClient = lw.rpcClient
	lw.mu.Unlock()

	// If the rpcClient is already set, you can just use that instead of attempting a new connection.
	if chainClient != nil {
		return
	}

	// rpcClient is not already set, attempt a new connection.
	networkAddress, err = NormalizeAddress(networkAddress, lw.activeNet.JSONRPCClientPort)
	if err != nil {
		return nil, errors.New(ErrInvalidAddress)
	}
	chainClient, err = chain.NewRPCClient(lw.activeNet.Params, networkAddress, username, password, cert, len(cert) == 0)
	if err != nil {
		return nil, translateError(err)
	}

	err = chainClient.Start(ctx, false)
	if err != nil {
		if err == rpcclient.ErrInvalidAuth {
			return nil, errors.New(ErrInvalid)
		}
		if errors.Match(errors.E(context.Canceled), err) {
			return nil, errors.New(ErrContextCanceled)
		}
		return nil, errors.New(ErrUnavailable)
	}

	// Set rpcClient so it can be used subsequently without re-connecting to the rpc server.
	lw.mu.Lock()
	lw.rpcClient = chainClient
	lw.mu.Unlock()

	return
}

func (lw *LibWallet) CancelSync(losePeers bool) {
	if lw.cancelSync != nil {
		lw.cancelSync() // will trigger context canceled in rpcSync or spvSync
		lw.cancelSync = nil
	}

	loadedWallet, walletLoaded := lw.walletLoader.LoadedWallet()
	if !walletLoaded {
		return
	}

	lw.walletLoader.SetNetworkBackend(nil)
	loadedWallet.SetNetworkBackend(nil)

	// It's important to wait to lose all peers when canceling sync
	// if the wallet database would be closed after canceling sync.
	if losePeers {
		log.Info("Waiting to lose all peers")
		lw.syncData.peersWG.Wait()
		log.Info("All peers are gone")
	}
}

func (lw *LibWallet) IsSynced() bool {
	return !lw.syncData.syncing && lw.connectedPeers != 0
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
		ctx, cancel := contextWithShutdownCancel(context.Background())
		lw.syncData.cancelRescan = cancel

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
				lw.syncData.cancelRescan = nil
				return
			default:
				continue
			}
		}

		// set this to nil, it no longer need since rescan has completed
		lw.syncData.cancelRescan = nil

		// Trigger sync completed callback.
		// todo: probably best to have a dedicated rescan listener
		// with callbacks for rescanStarted, rescanCompleted, rescanError and rescanCancel
		for _, syncProgressListener := range lw.syncProgressListeners {
			syncProgressListener.OnSyncCompleted()
		}
	}()

	return nil
}

func (lw *LibWallet) CancelRescan() {
	if lw.syncData.cancelRescan != nil {
		lw.syncData.cancelRescan()
		lw.syncData.cancelRescan = nil
		for _, syncProgressListener := range lw.syncProgressListeners {
			syncProgressListener.OnSyncCanceled()
		}
	}
}

func (lw *LibWallet) IsScanning() bool {
	return lw.syncData.rescanning
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
