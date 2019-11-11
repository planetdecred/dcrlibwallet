package dcrlibwallet

import (
	"math"
	"sync"
	"time"

	spv "github.com/raedahgroup/dcrlibwallet/spv"
)

const (
	SyncStateStart    = "start"
	SyncStateProgress = "progress"
	SyncStateFinish   = "finish"
)

func (mw *MultiWallet) spvSyncNotificationCallbacks() *spv.Notifications {
	return &spv.Notifications{
		PeerConnected: func(peerCount int32, addr string) {
			mw.handlePeerCountUpdate(peerCount)
		},
		PeerDisconnected: func(peerCount int32, addr string) {
			mw.handlePeerCountUpdate(peerCount)
		},
		Synced:                       mw.synced,
		FetchHeadersStarted:          mw.fetchHeadersStarted,
		FetchHeadersProgress:         mw.fetchHeadersProgress,
		FetchHeadersFinished:         mw.fetchHeadersFinished,
		FetchMissingCFiltersStarted:  func(walletID int) {},
		FetchMissingCFiltersProgress: func(walletID int, missingCFitlersStart, missingCFitlersEnd int32) {},
		FetchMissingCFiltersFinished: func(walletID int) {},
		DiscoverAddressesStarted:     mw.discoverAddressesStarted,
		DiscoverAddressesFinished:    mw.discoverAddressesFinished,
		RescanStarted:                mw.rescanStarted,
		RescanProgress:               mw.rescanProgress,
		RescanFinished:               mw.rescanFinished,
	}
}

func (mw *MultiWallet) handlePeerCountUpdate(peerCount int32) {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()
	mw.connectedPeers = peerCount
	for _, syncProgressListener := range mw.syncProgressListeners {
		syncProgressListener.OnPeerConnectedOrDisconnected(peerCount)
	}

	if mw.syncData.showLogs && mw.syncData.syncing {
		if peerCount == 1 {
			log.Infof("Connected to %d peer on %s.\n", peerCount, mw.activeNet.Name)
		} else {
			log.Infof("Connected to %d peers on %s.\n", peerCount, mw.activeNet.Name)
		}
	}
}

// Fetch Headers Callbacks

func (mw *MultiWallet) fetchHeadersStarted(peerInitialHeight int32) {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()
	if !mw.syncData.syncing || mw.beginFetchTimeStamp != -1 {
		// ignore if sync is not in progress i.e. !mw.syncData.syncing
		// or already started headers fetching i.e. mw.beginFetchTimeStamp != -1
		return
	}

	for _, lw := range mw.libWallets {
		lw.waiting = true
	}

	mw.activeSyncData.syncStage = HeadersFetchSyncStage
	mw.activeSyncData.beginFetchTimeStamp = time.Now().Unix()
	mw.activeSyncData.startHeaderHeight = mw.GetLowestBlock().Height
	mw.activeSyncData.totalFetchedHeadersCount = 0

	if mw.syncData.showLogs && mw.syncData.syncing {
		blockInfo := mw.GetLowestBlock()
		totalHeadersToFetch := mw.estimateBlockHeadersCountAfter(blockInfo.Timestamp)
		log.Infof("Step 1 of 3 - fetching %d block headers.\n", totalHeadersToFetch)
	}
}

func (mw *MultiWallet) fetchHeadersProgress(fetchedHeadersCount int32, lastHeaderTime int64) {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	if !mw.syncData.syncing || mw.activeSyncData.headersFetchTimeSpent != -1 {
		// Ignore this call because this function gets called for each peer and
		// we'd want to ignore those calls as far as the wallet is synced (i.e. !syncListener.syncing)
		// or headers are completely fetched (i.e. syncListener.headersFetchTimeSpent != -1)
		return
	}

	for _, lw := range mw.libWallets {
		if lw.GetBestBlock() <= fetchedHeadersCount {
			lw.waiting = false
		}
	}

	// If there was some period of inactivity,
	// assume that this process started at some point in the future,
	// thereby accounting for the total reported time of inactivity.
	mw.activeSyncData.beginFetchTimeStamp += mw.activeSyncData.totalInactiveSeconds
	mw.activeSyncData.totalInactiveSeconds = 0

	mw.activeSyncData.totalFetchedHeadersCount = fetchedHeadersCount
	headersLeftToFetch := mw.estimateBlockHeadersCountAfter(lastHeaderTime)
	totalHeadersToFetch := mw.activeSyncData.totalFetchedHeadersCount + headersLeftToFetch
	headersFetchProgress := float64(mw.activeSyncData.totalFetchedHeadersCount) / float64(totalHeadersToFetch)

	// update headers fetching progress report
	mw.activeSyncData.headersFetchProgress.TotalHeadersToFetch = totalHeadersToFetch
	mw.activeSyncData.headersFetchProgress.CurrentHeaderTimestamp = lastHeaderTime
	mw.activeSyncData.headersFetchProgress.FetchedHeadersCount = mw.activeSyncData.totalFetchedHeadersCount
	mw.activeSyncData.headersFetchProgress.HeadersFetchProgress = roundUp(headersFetchProgress * 100.0)

	timeTakenSoFar := time.Now().Unix() - mw.activeSyncData.beginFetchTimeStamp
	if timeTakenSoFar < 1 {
		timeTakenSoFar = 1
	}
	estimatedTotalHeadersFetchTime := float64(timeTakenSoFar) / headersFetchProgress

	// For some reason, the actual total headers fetch time is more than the predicted/estimated time.
	// Account for this difference by multiplying the estimatedTotalHeadersFetchTime by an incrementing factor.
	// The incrementing factor is inversely proportional to the headers fetch progress,
	// ranging from 0.5 to 0 as headers fetching progress increases from 0 to 1.
	adjustmentFactor := 0.5 * (1 - headersFetchProgress)
	estimatedTotalHeadersFetchTime += estimatedTotalHeadersFetchTime * adjustmentFactor

	estimatedDiscoveryTime := estimatedTotalHeadersFetchTime * DiscoveryPercentage
	estimatedRescanTime := estimatedTotalHeadersFetchTime * RescanPercentage
	estimatedTotalSyncTime := estimatedTotalHeadersFetchTime + estimatedDiscoveryTime + estimatedRescanTime

	// update total progress percentage and total time remaining
	totalSyncProgress := float64(timeTakenSoFar) / estimatedTotalSyncTime
	totalTimeRemainingSeconds := int64(math.Round(estimatedTotalSyncTime)) - timeTakenSoFar
	mw.activeSyncData.headersFetchProgress.TotalSyncProgress = roundUp(totalSyncProgress * 100.0)
	mw.activeSyncData.headersFetchProgress.TotalTimeRemainingSeconds = totalTimeRemainingSeconds

	// notify progress listener of estimated progress report
	mw.publishFetchHeadersProgress()

	headersFetchTimeRemaining := estimatedTotalHeadersFetchTime - float64(timeTakenSoFar)
	debugInfo := &DebugInfo{
		timeTakenSoFar,
		totalTimeRemainingSeconds,
		timeTakenSoFar,
		int64(math.Round(headersFetchTimeRemaining)),
	}
	mw.publishDebugInfo(debugInfo)
}

func (mw *MultiWallet) publishFetchHeadersProgress() {
	for _, syncProgressListener := range mw.syncData.syncProgressListeners {
		syncProgressListener.OnHeadersFetchProgress(&mw.headersFetchProgress)
	}
}

func (mw *MultiWallet) fetchHeadersFinished() {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	if !mw.syncData.syncing {
		// ignore if sync is not in progress
		return
	}

	mw.activeSyncData.startHeaderHeight = -1
	mw.activeSyncData.headersFetchTimeSpent = time.Now().Unix() - mw.beginFetchTimeStamp

	// If there is some period of inactivity reported at this stage,
	// subtract it from the total stage time.
	mw.activeSyncData.headersFetchTimeSpent -= mw.totalInactiveSeconds
	mw.activeSyncData.totalInactiveSeconds = 0

	if mw.activeSyncData.headersFetchTimeSpent < 150 {
		// This ensures that minimum ETA used for stage 2 (address discovery) is 120 seconds (80% of 150 seconds).
		mw.activeSyncData.headersFetchTimeSpent = 150
	}

	if mw.syncData.showLogs && mw.syncData.syncing {
		log.Info("Fetch headers completed.")
	}
}

// Address/Account Discovery Callbacks

func (mw *MultiWallet) discoverAddressesStarted(walletID int) {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	if !mw.syncData.syncing || mw.activeSyncData.addressDiscoveryCompleted != nil {
		// ignore if sync is not in progress i.e. !mw.syncData.syncing
		// or already started address discovery i.e. mw.activeSyncData.addressDiscoveryCompleted != nil
		return
	}

	mw.activeSyncData.syncStage = AddressDiscoverySyncStage
	mw.activeSyncData.addressDiscoveryStartTime = time.Now().Unix()
	mw.activeSyncData.addressDiscoveryProgress.WalletID = walletID
	if mw.syncData.showLogs && mw.syncData.syncing {
		log.Info("Step 2 of 3 - discovering used addresses.")
	}

	mw.updateAddressDiscoveryProgress()
}

func (mw *MultiWallet) updateAddressDiscoveryProgress() {
	// these values will be used every second to calculate the total sync progress
	totalHeadersFetchTime := float64(mw.headersFetchTimeSpent)
	estimatedDiscoveryTime := totalHeadersFetchTime * DiscoveryPercentage
	estimatedRescanTime := totalHeadersFetchTime * RescanPercentage

	// following channels are used to determine next step in the below subroutine
	everySecondTicker := time.NewTicker(1 * time.Second)
	everySecondTickerChannel := everySecondTicker.C

	// track last logged time remaining and total percent to avoid re-logging same message
	var lastTimeRemaining int64
	var lastTotalPercent int32 = -1

	mw.addressDiscoveryCompleted = make(chan bool)

	go func() {
		for {

			mw.syncData.mu.RLock()
			// If there was some period of inactivity,
			// assume that this process started at some point in the future,
			// thereby accounting for the total reported time of inactivity.
			mw.addressDiscoveryStartTime += mw.totalInactiveSeconds
			mw.totalInactiveSeconds = 0
			mw.syncData.mu.RUnlock()

			select {
			case <-everySecondTickerChannel:
				mw.syncData.mu.RLock()

				if mw.activeSyncData == nil {
					mw.syncData.mu.RUnlock()
					return
				}

				// calculate address discovery progress
				elapsedDiscoveryTime := float64(time.Now().Unix() - mw.addressDiscoveryStartTime)
				discoveryProgress := (elapsedDiscoveryTime / estimatedDiscoveryTime) * 100

				var totalSyncTime float64
				if elapsedDiscoveryTime > estimatedDiscoveryTime {
					totalSyncTime = totalHeadersFetchTime + elapsedDiscoveryTime + estimatedRescanTime
				} else {
					totalSyncTime = totalHeadersFetchTime + estimatedDiscoveryTime + estimatedRescanTime
				}

				totalElapsedTime := totalHeadersFetchTime + elapsedDiscoveryTime
				totalProgress := (totalElapsedTime / totalSyncTime) * 100

				remainingAccountDiscoveryTime := math.Round(estimatedDiscoveryTime - elapsedDiscoveryTime)
				if remainingAccountDiscoveryTime < 0 {
					remainingAccountDiscoveryTime = 0
				}

				totalProgressPercent := int32(math.Round(totalProgress))
				totalTimeRemainingSeconds := int64(math.Round(remainingAccountDiscoveryTime + estimatedRescanTime))

				// update address discovery progress, total progress and total time remaining
				mw.addressDiscoveryProgress.AddressDiscoveryProgress = int32(math.Round(discoveryProgress))
				mw.addressDiscoveryProgress.TotalSyncProgress = totalProgressPercent
				mw.addressDiscoveryProgress.TotalTimeRemainingSeconds = totalTimeRemainingSeconds

				mw.publishAddressDiscoveryProgress()

				debugInfo := &DebugInfo{
					int64(math.Round(totalElapsedTime)),
					totalTimeRemainingSeconds,
					int64(math.Round(elapsedDiscoveryTime)),
					int64(math.Round(remainingAccountDiscoveryTime)),
				}
				mw.publishDebugInfo(debugInfo)

				if mw.showLogs && mw.syncing {
					// avoid logging same message multiple times
					if totalProgressPercent != lastTotalPercent || totalTimeRemainingSeconds != lastTimeRemaining {
						log.Infof("Syncing %d%%, %s remaining, discovering used addresses.\n",
							totalProgressPercent, CalculateTotalTimeRemaining(totalTimeRemainingSeconds))

						lastTotalPercent = totalProgressPercent
						lastTimeRemaining = totalTimeRemainingSeconds
					}
				}
				mw.syncData.mu.RUnlock()
			case <-mw.addressDiscoveryCompleted:
				// stop updating time taken and progress for address discovery
				everySecondTicker.Stop()

				if mw.showLogs && mw.syncing {
					log.Info("Address discovery complete.")
				}

				return
			}
		}
	}()
}

func (mw *MultiWallet) publishAddressDiscoveryProgress() {
	if !mw.syncData.syncing {
		// ignore if sync is not in progress
		return
	}

	for _, syncProgressListener := range mw.syncData.syncProgressListeners {
		syncProgressListener.OnAddressDiscoveryProgress(&mw.activeSyncData.addressDiscoveryProgress)
	}
}

func (mw *MultiWallet) discoverAddressesFinished(walletID int) {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()
	if !mw.syncData.syncing {
		// ignore if sync is not in progress
		return
	}

	addressDiscoveryFinishTime := time.Now().Unix()
	mw.activeSyncData.totalDiscoveryTimeSpent = addressDiscoveryFinishTime - mw.addressDiscoveryStartTime

	close(mw.activeSyncData.addressDiscoveryCompleted)
	mw.activeSyncData.addressDiscoveryCompleted = nil

	lw := mw.libWallets[walletID]
	loadedWallet, loaded := lw.walletLoader.LoadedWallet()
	if loaded { // loaded should always be through
		if !loadedWallet.Locked() {
			loadedWallet.Lock()
			err := mw.discoveredAccounts(walletID)
			if err != nil {
				log.Error(err)
			}
		}
	}

}

// Blocks Scan Callbacks

func (mw *MultiWallet) rescanStarted(walletID int) {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	if !mw.syncData.syncing {
		// ignore if sync is not in progress
		return
	}

	if mw.activeSyncData.addressDiscoveryCompleted != nil {
		close(mw.activeSyncData.addressDiscoveryCompleted)
		mw.activeSyncData.addressDiscoveryCompleted = nil
	}

	mw.activeSyncData.syncStage = HeadersRescanSyncStage
	mw.activeSyncData.rescanStartTime = time.Now().Unix()

	// retain last total progress report from address discovery phase
	mw.activeSyncData.headersRescanProgress.TotalTimeRemainingSeconds = mw.activeSyncData.addressDiscoveryProgress.TotalTimeRemainingSeconds
	mw.activeSyncData.headersRescanProgress.TotalSyncProgress = mw.activeSyncData.addressDiscoveryProgress.TotalSyncProgress
	mw.activeSyncData.headersRescanProgress.WalletID = walletID

	if mw.syncData.showLogs && mw.syncData.syncing {
		log.Info("Step 3 of 3 - Scanning block headers")
	}
}

func (mw *MultiWallet) rescanProgress(walletID int, rescannedThrough int32) {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	if !mw.syncData.syncing {
		// ignore if sync is not in progress
		return
	}

	lw := mw.libWallets[walletID]

	mw.activeSyncData.headersRescanProgress.WalletID = walletID
	mw.activeSyncData.headersRescanProgress.TotalHeadersToScan = lw.GetBestBlock()

	rescanRate := float64(rescannedThrough) / float64(mw.activeSyncData.headersRescanProgress.TotalHeadersToScan)
	mw.activeSyncData.headersRescanProgress.RescanProgress = int32(math.Round(rescanRate * 100))
	mw.activeSyncData.headersRescanProgress.CurrentRescanHeight = rescannedThrough

	// If there was some period of inactivity,
	// assume that this process started at some point in the future,
	// thereby accounting for the total reported time of inactivity.
	mw.activeSyncData.rescanStartTime += mw.activeSyncData.totalInactiveSeconds
	mw.activeSyncData.totalInactiveSeconds = 0

	elapsedRescanTime := time.Now().Unix() - mw.activeSyncData.rescanStartTime
	totalElapsedTime := mw.activeSyncData.headersFetchTimeSpent + mw.activeSyncData.totalDiscoveryTimeSpent + elapsedRescanTime

	estimatedTotalRescanTime := int64(math.Round(float64(elapsedRescanTime) / rescanRate))
	mw.activeSyncData.headersRescanProgress.RescanTimeRemaining = estimatedTotalRescanTime - elapsedRescanTime
	totalTimeRemainingSeconds := mw.activeSyncData.headersRescanProgress.RescanTimeRemaining

	// do not update total time taken and total progress percent if elapsedRescanTime is 0
	// because the estimatedTotalRescanTime will be inaccurate (also 0)
	// which will make the estimatedTotalSyncTime equal to totalElapsedTime
	// giving the wrong impression that the process is complete
	if elapsedRescanTime > 0 {
		estimatedTotalSyncTime := mw.activeSyncData.headersFetchTimeSpent + mw.activeSyncData.totalDiscoveryTimeSpent + estimatedTotalRescanTime
		totalProgress := (float64(totalElapsedTime) / float64(estimatedTotalSyncTime)) * 100

		mw.activeSyncData.headersRescanProgress.TotalTimeRemainingSeconds = totalTimeRemainingSeconds
		mw.activeSyncData.headersRescanProgress.TotalSyncProgress = int32(math.Round(totalProgress))
	}

	mw.publishHeadersRescanProgress()

	debugInfo := &DebugInfo{
		totalElapsedTime,
		totalTimeRemainingSeconds,
		elapsedRescanTime,
		mw.activeSyncData.headersRescanProgress.RescanTimeRemaining,
	}
	mw.publishDebugInfo(debugInfo)

	if mw.syncData.showLogs && mw.syncData.syncing {
		log.Infof("Syncing %d%%, %s remaining, scanning %d of %d block headers.\n",
			mw.activeSyncData.headersRescanProgress.TotalSyncProgress,
			CalculateTotalTimeRemaining(mw.activeSyncData.headersRescanProgress.TotalTimeRemainingSeconds),
			mw.activeSyncData.headersRescanProgress.CurrentRescanHeight,
			mw.activeSyncData.headersRescanProgress.TotalHeadersToScan,
		)
	}
}

func (mw *MultiWallet) publishHeadersRescanProgress() {
	for _, syncProgressListener := range mw.syncData.syncProgressListeners {
		syncProgressListener.OnHeadersRescanProgress(&mw.activeSyncData.headersRescanProgress)
	}
}

func (mw *MultiWallet) rescanFinished(walletID int) {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	if !mw.syncData.syncing {
		// ignore if sync is not in progress
		return
	}

	mw.activeSyncData.headersRescanProgress.WalletID = walletID
	mw.activeSyncData.headersRescanProgress.TotalTimeRemainingSeconds = 0
	mw.activeSyncData.headersRescanProgress.TotalSyncProgress = 100
	mw.publishHeadersRescanProgress()
}

func (mw *MultiWallet) publishDebugInfo(debugInfo *DebugInfo) {
	for _, syncProgressListener := range mw.syncData.syncProgressListeners {
		syncProgressListener.Debug(debugInfo)
	}
}

// Transaction Notification

func (mw *MultiWallet) mempoolTransactionNotification(transaction string) {
	for _, syncProgressListener := range mw.syncData.syncProgressListeners {
		syncProgressListener.OnTransaction(transaction)
	}
}

func (mw *MultiWallet) publishTransactionConfirmed(walletID int, transactionHash string, blockHeight int32) {
	for _, syncProgressListener := range mw.syncData.syncProgressListeners {
		syncProgressListener.OnTransactionConfirmed(walletID, transactionHash)
	}
}

/** Helper functions start here */

func (mw *MultiWallet) estimateBlockHeadersCountAfter(lastHeaderTime int64) int32 {
	// Use the difference between current time (now) and last reported block time, to estimate total headers to fetch
	timeDifference := time.Now().Unix() - lastHeaderTime
	estimatedHeadersDifference := float64(timeDifference) / float64(mw.activeSyncData.targetTimePerBlock)

	// return next integer value (upper limit) if estimatedHeadersDifference is a fraction
	return int32(math.Ceil(estimatedHeadersDifference))
}

func (mw *MultiWallet) notifySyncError(code SyncErrorCode, err error) {
	mw.resetSyncData()

	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	for _, syncProgressListener := range mw.syncData.syncProgressListeners {
		syncProgressListener.OnSyncEndedWithError(err)
	}
}

func (mw *MultiWallet) notifySyncCanceled() {
	mw.resetSyncData()

	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	for _, syncProgressListener := range mw.syncData.syncProgressListeners {
		syncProgressListener.OnSyncCanceled(mw.syncData.restartSyncRequested)
	}
}

func (mw *MultiWallet) resetSyncData() {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	mw.syncData.syncing = false
	mw.syncData.synced = false
	mw.activeSyncData = nil // to be reintialized on next sync

	for _, lw := range mw.libWallets {
		lw.waiting = true
	}
}

func (mw *MultiWallet) synced(walletID int, synced bool) {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	lw := mw.libWallets[walletID]
	lw.synced = synced
	lw.syncing = false
	if mw.OpenedWalletsCount() == mw.SyncedWalletsCount() {
		mw.syncData.syncing = false
		mw.syncData.synced = true
		mw.activeSyncData = nil // to be reintialized on next sync

		for _, syncProgressListener := range mw.syncProgressListeners {
			if synced {
				syncProgressListener.OnSyncCompleted()
			} else {
				syncProgressListener.OnSyncCanceled(false)
			}
		}

		// begin indexing transactions after sync is completed,
		// syncProgressListeners.OnSynced() will be invoked after transactions are indexed
		var waitForIndexing sync.WaitGroup
		waitForIndexing.Add(len(mw.libWallets))
		for _, lw := range mw.libWallets {
			lw.IndexTransactions(&waitForIndexing)
		}

		go func() {
			waitForIndexing.Wait()
			for _, syncProgressListener := range mw.syncProgressListeners {
				if mw.IsSynced() {
					syncProgressListener.OnSyncCompleted()
				} else {
					syncProgressListener.OnSyncCanceled(false)
				}
			}
		}()
	}
}
