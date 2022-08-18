package dcr

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"

	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/p2p"
	w "decred.org/dcrwallet/v2/wallet"
	"github.com/decred/dcrd/addrmgr/v2"
	"github.com/planetdecred/dcrlibwallet/spv"
)

// reading/writing of properties of this struct are protected by mutex.x
type SyncData struct {
	mu sync.RWMutex

	SyncProgressListeners map[string]SyncProgressListener
	showLogs              bool

	synced       bool
	syncing      bool
	cancelSync   context.CancelFunc
	cancelRescan context.CancelFunc
	syncCanceled chan struct{}

	// Flag to notify syncCanceled callback if the sync was canceled so as to be restarted.
	restartSyncRequested bool

	rescanning     bool
	connectedPeers int32

	*activeSyncData
}

// reading/writing of properties of this struct are protected by syncData.mu.
type activeSyncData struct {
	syncer *spv.Syncer

	syncStage int32

	cfiltersFetchProgress    CFiltersFetchProgressReport
	headersFetchProgress     HeadersFetchProgressReport
	addressDiscoveryProgress AddressDiscoveryProgressReport
	headersRescanProgress    HeadersRescanProgressReport

	addressDiscoveryCompletedOrCanceled chan bool

	rescanStartTime int64

	totalInactiveSeconds int64
}

const (
	InvalidSyncStage          = -1
	CFiltersFetchSyncStage    = 0
	HeadersFetchSyncStage     = 1
	AddressDiscoverySyncStage = 2
	HeadersRescanSyncStage    = 3
)

func (wallet *Wallet) initActiveSyncData() {

	cfiltersFetchProgress := CFiltersFetchProgressReport{
		GeneralSyncProgress:         &GeneralSyncProgress{},
		beginFetchCFiltersTimeStamp: 0,
		startCFiltersHeight:         -1,
		cfiltersFetchTimeSpent:      0,
		totalFetchedCFiltersCount:   0,
	}

	headersFetchProgress := HeadersFetchProgressReport{
		GeneralSyncProgress:      &GeneralSyncProgress{},
		beginFetchTimeStamp:      -1,
		headersFetchTimeSpent:    -1,
		totalFetchedHeadersCount: 0,
	}

	addressDiscoveryProgress := AddressDiscoveryProgressReport{
		GeneralSyncProgress:       &GeneralSyncProgress{},
		addressDiscoveryStartTime: -1,
		totalDiscoveryTimeSpent:   -1,
	}

	headersRescanProgress := HeadersRescanProgressReport{}
	headersRescanProgress.GeneralSyncProgress = &GeneralSyncProgress{}

	wallet.syncData.mu.Lock()
	wallet.syncData.activeSyncData = &activeSyncData{
		syncStage: InvalidSyncStage,

		cfiltersFetchProgress:    cfiltersFetchProgress,
		headersFetchProgress:     headersFetchProgress,
		addressDiscoveryProgress: addressDiscoveryProgress,
		headersRescanProgress:    headersRescanProgress,
	}
	wallet.syncData.mu.Unlock()
}

func (wallet *Wallet) IsSyncProgressListenerRegisteredFor(uniqueIdentifier string) bool {
	wallet.syncData.mu.RLock()
	_, exists := wallet.syncData.SyncProgressListeners[uniqueIdentifier]
	wallet.syncData.mu.RUnlock()
	return exists
}

func (wallet *Wallet) AddSyncProgressListener(syncProgressListener SyncProgressListener, uniqueIdentifier string) error {
	if wallet.IsSyncProgressListenerRegisteredFor(uniqueIdentifier) {
		return errors.New(ErrListenerAlreadyExist)
	}

	wallet.syncData.mu.Lock()
	wallet.syncData.SyncProgressListeners[uniqueIdentifier] = syncProgressListener
	wallet.syncData.mu.Unlock()

	// If sync is already on, notify this newly added listener of the current progress report.
	return wallet.PublishLastSyncProgress(uniqueIdentifier)
}

func (wallet *Wallet) RemoveSyncProgressListener(uniqueIdentifier string) {
	wallet.syncData.mu.Lock()
	delete(wallet.syncData.SyncProgressListeners, uniqueIdentifier)
	wallet.syncData.mu.Unlock()
}

func (wallet *Wallet) syncProgressListeners() []SyncProgressListener {
	wallet.syncData.mu.RLock()
	defer wallet.syncData.mu.RUnlock()

	listeners := make([]SyncProgressListener, 0, len(wallet.syncData.SyncProgressListeners))
	for _, listener := range wallet.syncData.SyncProgressListeners {
		listeners = append(listeners, listener)
	}

	return listeners
}

func (wallet *Wallet) PublishLastSyncProgress(uniqueIdentifier string) error {
	wallet.syncData.mu.RLock()
	defer wallet.syncData.mu.RUnlock()

	syncProgressListener, exists := wallet.syncData.SyncProgressListeners[uniqueIdentifier]
	if !exists {
		return errors.New(ErrInvalid)
	}

	if wallet.syncData.syncing && wallet.syncData.activeSyncData != nil {
		switch wallet.syncData.activeSyncData.syncStage {
		case HeadersFetchSyncStage:
			syncProgressListener.OnHeadersFetchProgress(&wallet.syncData.headersFetchProgress)
		case AddressDiscoverySyncStage:
			syncProgressListener.OnAddressDiscoveryProgress(&wallet.syncData.addressDiscoveryProgress)
		case HeadersRescanSyncStage:
			syncProgressListener.OnHeadersRescanProgress(&wallet.syncData.headersRescanProgress)
		}
	}

	return nil
}

func (wallet *Wallet) EnableSyncLogs() {
	wallet.syncData.mu.Lock()
	wallet.syncData.showLogs = true
	wallet.syncData.mu.Unlock()
}

func (wallet *Wallet) SyncInactiveForPeriod(totalInactiveSeconds int64) {
	wallet.syncData.mu.Lock()
	defer wallet.syncData.mu.Unlock()

	if !wallet.syncData.syncing || wallet.syncData.activeSyncData == nil {
		log.Debug("Not accounting for inactive time, wallet is not syncing.")
		return
	}

	wallet.syncData.totalInactiveSeconds += totalInactiveSeconds
	if wallet.syncData.connectedPeers == 0 {
		// assume it would take another 60 seconds to reconnect to peers
		wallet.syncData.totalInactiveSeconds += 60
	}
}

func (wallet *Wallet) SpvSync() error {
	// prevent an attempt to sync when the previous syncing has not been canceled
	if wallet.IsSyncing() || wallet.IsSynced() {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
	addrManager := addrmgr.New(wallet.rootDir, net.LookupIP) // TODO: be mindful of tor
	lp := p2p.NewLocalPeer(wallet.chainParams, addr, addrManager)

	var validPeerAddresses []string
	peerAddresses := wallet.ReadStringConfigValueForKey(SpvPersistentPeerAddressesConfigKey, "")
	if peerAddresses != "" {
		addresses := strings.Split(peerAddresses, ";")
		for _, address := range addresses {
			peerAddress, err := NormalizeAddress(address, wallet.chainParams.DefaultPort)
			if err != nil {
				log.Errorf("SPV peer address(%s) is invalid: %v", peerAddress, err)
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
	wallet.initActiveSyncData()

	wallets := make(map[int]*w.Wallet)
	wallets[wallet.ID] = wallet.Internal()
	wallet.WaitingForHeaders = true
	wallet.Syncing = true

	syncer := spv.NewSyncer(wallets, lp)
	syncer.SetNotifications(wallet.spvSyncNotificationCallbacks())
	if len(validPeerAddresses) > 0 {
		syncer.SetPersistentPeers(validPeerAddresses)
	}

	ctx, cancel := wallet.contextWithShutdownCancel()

	var restartSyncRequested bool

	wallet.syncData.mu.Lock()
	restartSyncRequested = wallet.syncData.restartSyncRequested
	wallet.syncData.restartSyncRequested = false
	wallet.syncData.syncing = true
	wallet.syncData.cancelSync = cancel
	wallet.syncData.syncCanceled = make(chan struct{})
	wallet.syncData.syncer = syncer
	wallet.syncData.mu.Unlock()

	for _, listener := range wallet.syncProgressListeners() {
		listener.OnSyncStarted(restartSyncRequested)
	}

	// syncer.Run uses a wait group to block the thread until the sync context
	// expires or is canceled or some other error occurs such as
	// losing connection to all persistent peers.
	go func() {
		syncError := syncer.Run(ctx)
		//sync has ended or errored
		if syncError != nil {
			if syncError == context.DeadlineExceeded {
				wallet.notifySyncError(errors.Errorf("SPV synchronization deadline exceeded: %v", syncError))
			} else if syncError == context.Canceled {
				close(wallet.syncData.syncCanceled)
				wallet.notifySyncCanceled()
			} else {
				wallet.notifySyncError(syncError)
			}
		}

		//reset sync variables
		wallet.resetSyncData()
	}()
	return nil
}

func (wallet *Wallet) RestartSpvSync() error {
	wallet.syncData.mu.Lock()
	wallet.syncData.restartSyncRequested = true
	wallet.syncData.mu.Unlock()

	wallet.CancelSync() // necessary to unset the network backend.
	return wallet.SpvSync()
}

func (wallet *Wallet) CancelSync() {
	wallet.syncData.mu.RLock()
	cancelSync := wallet.syncData.cancelSync
	wallet.syncData.mu.RUnlock()

	if cancelSync != nil {
		log.Info("Canceling sync. May take a while for sync to fully cancel.")

		// Stop running cspp mixers
		if wallet.IsAccountMixerActive() {
			log.Infof("[%d] Stopping cspp mixer", wallet.ID)
			err := wallet.StopAccountMixer()
			if err != nil {
				log.Errorf("[%d] Error stopping cspp mixer: %v", wallet.ID, err)
			}
		}

		// Cancel the context used for syncer.Run in spvSync().
		// This may not immediately cause the sync process to terminate,
		// but when it eventually terminates, syncer.Run will return `err == context.Canceled`.
		cancelSync()

		// When sync terminates and syncer.Run returns `err == context.Canceled`,
		// we will get notified on this channel.
		<-wallet.syncData.syncCanceled

		log.Info("Sync fully canceled.")
	}
}

func (wallet *Wallet) IsWaiting() bool {
	return wallet.WaitingForHeaders
}

func (wallet *Wallet) IsSynced() bool {
	return wallet.Synced
}

func (wallet *Wallet) IsSyncing() bool {
	return wallet.Syncing
}

func (wallet *Wallet) IsConnectedToDecredNetwork() bool {
	wallet.syncData.mu.RLock()
	defer wallet.syncData.mu.RUnlock()
	return wallet.syncData.syncing || wallet.syncData.synced
}

// func (wallet *Wallet) IsSynced() bool {
// 	wallet.syncData.mu.RLock()
// 	defer wallet.syncData.mu.RUnlock()
// 	return wallet.syncData.synced
// }

// func (wallet *Wallet) IsSyncing() bool {
// 	wallet.syncData.mu.RLock()
// 	defer wallet.syncData.mu.RUnlock()
// 	return wallet.syncData.syncing
// }

func (wallet *Wallet) CurrentSyncStage() int32 {
	wallet.syncData.mu.RLock()
	defer wallet.syncData.mu.RUnlock()

	if wallet.syncData != nil && wallet.syncData.syncing {
		return wallet.syncData.syncStage
	}
	return InvalidSyncStage
}

func (wallet *Wallet) GeneralSyncProgress() *GeneralSyncProgress {
	wallet.syncData.mu.RLock()
	defer wallet.syncData.mu.RUnlock()

	if wallet.syncData != nil && wallet.syncData.syncing {
		switch wallet.syncData.syncStage {
		case HeadersFetchSyncStage:
			return wallet.syncData.headersFetchProgress.GeneralSyncProgress
		case AddressDiscoverySyncStage:
			return wallet.syncData.addressDiscoveryProgress.GeneralSyncProgress
		case HeadersRescanSyncStage:
			return wallet.syncData.headersRescanProgress.GeneralSyncProgress
		case CFiltersFetchSyncStage:
			return wallet.syncData.cfiltersFetchProgress.GeneralSyncProgress
		}
	}

	return nil
}

func (wallet *Wallet) ConnectedPeers() int32 {
	wallet.syncData.mu.RLock()
	defer wallet.syncData.mu.RUnlock()
	return wallet.syncData.connectedPeers
}

func (wallet *Wallet) PeerInfoRaw() ([]PeerInfo, error) {
	if !wallet.IsConnectedToDecredNetwork() {
		return nil, errors.New(ErrNotConnected)
	}

	syncer := wallet.syncData.syncer

	infos := make([]PeerInfo, 0, len(syncer.GetRemotePeers()))
	for _, rp := range syncer.GetRemotePeers() {
		info := PeerInfo{
			ID:             int32(rp.ID()),
			Addr:           rp.RemoteAddr().String(),
			AddrLocal:      rp.LocalAddr().String(),
			Services:       fmt.Sprintf("%08d", uint64(rp.Services())),
			Version:        rp.Pver(),
			SubVer:         rp.UA(),
			StartingHeight: int64(rp.InitialHeight()),
			BanScore:       int32(rp.BanScore()),
		}

		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ID < infos[j].ID
	})

	return infos, nil
}

func (wallet *Wallet) PeerInfo() (string, error) {
	infos, err := wallet.PeerInfoRaw()
	if err != nil {
		return "", err
	}

	result, _ := json.Marshal(infos)
	return string(result), nil
}

// func (wallet *Wallet) GetBestBlock() *BlockInfo {
// 	var bestBlock int32 = -1
// 	var blockInfo *BlockInfo
// 	for _, wallet := range wallet.wallets {
// 		if !wallet.WalletOpened() {
// 			continue
// 		}

// 		walletBestBLock := wallet.GetBestBlock()
// 		if walletBestBLock > bestBlock || bestBlock == -1 {
// 			bestBlock = walletBestBLock
// 			blockInfo = &BlockInfo{Height: bestBlock, Timestamp: wallet.GetBestBlockTimeStamp()}
// 		}
// 	}

// 	return blockInfo
// }

func (wallet *Wallet) GetLowestBlock() *BlockInfo {
	var lowestBlock int32 = -1
	var blockInfo *BlockInfo
	// for _, wallet := range wallet.wallets {
	if !wallet.WalletOpened() {
		return nil
	}
	walletBestBLock := wallet.GetBestBlock()
	if walletBestBLock < lowestBlock || lowestBlock == -1 {
		lowestBlock = walletBestBLock
		blockInfo = &BlockInfo{Height: lowestBlock, Timestamp: wallet.GetBestBlockTimeStamp()}
	}
	// }

	return blockInfo
}

func (wallet *Wallet) GetBestBlock() int32 {
	if wallet.Internal() == nil {
		// This method is sometimes called after a wallet is deleted and causes crash.
		log.Error("Attempting to read best block height without a loaded wallet.")
		return 0
	}

	_, height := wallet.Internal().MainChainTip(wallet.ShutdownContext())
	return height
}

func (wallet *Wallet) GetBestBlockTimeStamp() int64 {
	if wallet.Internal() == nil {
		// This method is sometimes called after a wallet is deleted and causes crash.
		log.Error("Attempting to read best block timestamp without a loaded wallet.")
		return 0
	}

	ctx := wallet.ShutdownContext()
	_, height := wallet.Internal().MainChainTip(ctx)
	identifier := w.NewBlockIdentifierFromHeight(height)
	info, err := wallet.Internal().BlockInfo(ctx, identifier)
	if err != nil {
		log.Error(err)
		return 0
	}
	return info.Timestamp
}

// func (wallet *Wallet) GetLowestBlockTimestamp() int64 {
// 	var timestamp int64 = -1
// 	for _, wallet := range wallet.wallets {
// 		bestBlockTimestamp := wallet.GetBestBlockTimeStamp()
// 		if bestBlockTimestamp < timestamp || timestamp == -1 {
// 			timestamp = bestBlockTimestamp
// 		}
// 	}
// 	return timestamp
// }
