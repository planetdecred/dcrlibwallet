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
	"github.com/raedahgroup/dcrlibwallet/utils"
)

type syncData struct {
	mu                    sync.Mutex
	rpcClient             *chain.RPCClient
	cancelSync            context.CancelFunc
	syncProgressListeners map[string]SyncProgressListener

	rescanning bool
	syncing    bool
	showLogs   bool
	synced     bool

	*activeSyncData
	connectedPeers int32

	// Flag to notify syncCanceled callback if the sync was canceled so as to be restarted.
	restartSyncRequested bool
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

// SpvSync loads a wallet to be synced via spv
func (lw *LibWallet) SpvSync(peerAddresses string) error {
	loadedWallet, err := lw.getLoadedWalletForSyncing()
	if err != nil {
		return err
	}

	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
	addrManager := addrmgr.New(lw.walletDataDir, net.LookupIP) // TODO: be mindful of tor
	lp := p2p.NewLocalPeer(loadedWallet.ChainParams(), addr, addrManager)

	var validPeerAddresses []string
	if peerAddresses != "" {
		addresses := strings.Split(peerAddresses, ";")
		for _, address := range addresses {
			peerAddress, err := utils.NormalizeAddress(address, lw.activeNet.Params.DefaultPort)
			if err != nil {
				lw.notifySyncError(ErrorCodeInvalidPeerAddress, errors.E("SPV peer address invalid: %v", err))
			} else {
				validPeerAddresses = append(validPeerAddresses, peerAddress)
			}
		}

		if len(validPeerAddresses) == 0 {
			return errors.New(ErrInvalidPeers)
		}
	}

	syncer := spv.NewSyncer(loadedWallet, lp)
	syncer.SetNotifications(lw.spvSyncNotificationCallbacks(loadedWallet))
	if len(validPeerAddresses) > 0 {
		syncer.SetPersistantPeers(validPeerAddresses)
	}

	loadedWallet.SetNetworkBackend(syncer)
	lw.walletLoader.SetNetworkBackend(syncer)

	ctx, cancel := contextWithShutdownCancel(context.Background())
	lw.cancelSync = cancel

	// syncer.Run uses a wait group to block the thread until defaultsynclistener completes or an error occurs
	go func() {
		err := syncer.Run(ctx)
		if err != nil {
			if err == context.Canceled {
				lw.notifySyncError(ErrorCodeContextCanceled, errors.E("SPV synchronization canceled: %v", err))
			} else if err == context.DeadlineExceeded {
				lw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("SPV synchronization deadline exceeded: %v", err))
			} else {
				lw.notifySyncError(ErrorCodeUnexpectedError, err)
			}
		}
	}()

	return nil
}

// RemoveSyncProgressListener deletes an  element with specified key `uniqueIdentifier`
// from the map `syncProgressListeners` in LibWallet
func (lw *LibWallet) RemoveSyncProgressListener(uniqueIdentifier string) {
	_, k := lw.syncProgressListeners[uniqueIdentifier]
	if k {
		delete(lw.syncProgressListeners, uniqueIdentifier)
	}
}

// EnableSyncLogs enables display of logs when syncing a wallet
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

// RestartSpvSync  restarts an spv process after cancelling
func (lw *LibWallet) RestartSpvSync(peerAddresses string) error {
	lw.syncData.restartSyncRequested = true
	lw.CancelSync() // necessary to unset the network backend.
	return lw.SpvSync(peerAddresses)
}

// RpcSync loads a wallet to be synced via RPC connection
func (lw *LibWallet) RpcSync(networkAddress string, username string, password string, cert []byte) error {
	loadedWallet, err := lw.getLoadedWalletForSyncing()
	if err != nil {
		return err
	}

	ctx, cancel := contextWithShutdownCancel(context.Background())
	lw.cancelSync = cancel

	chainClient, err := lw.connectToRpcClient(ctx, networkAddress, username, password, cert)
	if err != nil {
		return err
	}

	syncer := chain.NewRPCSyncer(loadedWallet, chainClient)
	syncer.SetNotifications(lw.generalSyncNotificationCallbacks(loadedWallet))

	networkBackend := chain.BackendFromRPCClient(chainClient.Client)
	lw.walletLoader.SetNetworkBackend(networkBackend)
	loadedWallet.SetNetworkBackend(networkBackend)

	// notify sync progress listeners that connected peer count will not be reported because we're using rpc
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnPeerDisconnected(-1)
	}

	// syncer.Run uses a wait group to block the thread until sync completes or an error occurs
	go func() {
		err := syncer.Run(ctx, true)
		if err != nil {
			if err == context.Canceled {
				lw.notifySyncError(ErrorCodeContextCanceled, errors.E("RPC synchronization canceled: %v", err))
			} else if err == context.DeadlineExceeded {
				lw.notifySyncError(ErrorCodeDeadlineExceeded, errors.E("RPC synchronization deadline exceeded: %v", err))
			} else {
				lw.notifySyncError(ErrorCodeUnexpectedError, err)
			}
		}
	}()

	return nil
}

// connectToRpcClient creates an direct connection to the RPC server
// described by the networkAddress string and attempts to establish a
// client connection to the remote server
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
	networkAddress, err = utils.NormalizeAddress(networkAddress, lw.activeNet.JSONRPCClientPort)
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

// getLoadedWalletForSyncing returns a loaded wallet to be synced
func (lw *LibWallet) getLoadedWalletForSyncing() (*wallet.Wallet, error) {
	loadedWallet, walletLoaded := lw.walletLoader.LoadedWallet()
	if walletLoaded {
		// Error if the wallet is already syncing with the network.
		currentNetworkBackend, _ := loadedWallet.NetworkBackend()
		if currentNetworkBackend != nil {
			return nil, errors.New(ErrSyncAlreadyInProgress)
		}
	} else {
		return nil, errors.New(ErrWalletNotLoaded)
	}
	return loadedWallet, nil
}

func (lw *LibWallet) CancelSync() {
	if lw.cancelSync != nil {
		lw.cancelSync()
	}

	for _, syncResponse := range lw.syncProgressListeners {
		syncResponse.OnSynced(false)
	}
}

// RescanBlocks starts a rescan of the wallet for all blocks
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
		ctx, _ := contextWithShutdownCancel(context.Background())

		var totalHeight int32
		go lw.wallet.RescanProgressFromHeight(ctx, netBackend, 0, progress)

		for p := range progress {
			if p.Err != nil {
				log.Error(p.Err)

				return
			}
			totalHeight += p.ScannedThrough
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(p.ScannedThrough, SyncStateProgress)
			}
		}

		select {
		case <-ctx.Done():
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(totalHeight, SyncStateProgress)
			}
		default:
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(totalHeight, SyncStateFinish)
			}
		}
	}()

	return nil
}

//IsSynced checks if a wallet is synced.
// It returns true is a wallet is synced and false if it's not
func (lw *LibWallet) IsSynced() bool {
	return lw.syncData.synced
}

func (lw *LibWallet) IsSyncing() bool {
	return lw.syncData.syncing
}

// GetBestBlock returns the height of the tip-most block
// in the main chain that the wallet is synchronized to.
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
