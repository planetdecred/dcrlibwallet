package dcrlibwallet

import (
	"math"
	"time"

	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/spv"
	"github.com/decred/dcrwallet/wallet"
)

const (
	SyncStateStart    = "start"
	SyncStateProgress = "progress"
	SyncStateFinish   = "finish"
)

func (lw *LibWallet) spvSyncNotificationCallbacks(loadedWallet *wallet.Wallet) *spv.Notifications {
	generalNotifications := lw.generalSyncNotificationCallbacks(loadedWallet)
	return &spv.Notifications{
		Synced:                       generalNotifications.Synced,
		FetchHeadersStarted:          generalNotifications.FetchHeadersStarted,
		FetchHeadersProgress:         generalNotifications.FetchHeadersProgress,
		FetchHeadersFinished:         generalNotifications.FetchHeadersFinished,
		FetchMissingCFiltersStarted:  generalNotifications.FetchMissingCFiltersStarted,
		FetchMissingCFiltersProgress: generalNotifications.FetchMissingCFiltersProgress,
		FetchMissingCFiltersFinished: generalNotifications.FetchMissingCFiltersFinished,
		DiscoverAddressesStarted:     generalNotifications.DiscoverAddressesStarted,
		DiscoverAddressesFinished:    generalNotifications.DiscoverAddressesFinished,
		RescanStarted:                generalNotifications.RescanStarted,
		RescanProgress:               generalNotifications.RescanProgress,
		RescanFinished:               generalNotifications.RescanFinished,
		PeerDisconnected: func(peerCount int32, addr string) {
			lw.handlePeerCountUpdate(peerCount)
		},
		PeerConnected: func(peerCount int32, addr string) {
			lw.handlePeerCountUpdate(peerCount)
		},
	}
}

func (lw *LibWallet) generalSyncNotificationCallbacks(loadedWallet *wallet.Wallet) *chain.Notifications {
	return &chain.Notifications{
		Synced: func(synced bool) {

			lw.beginFetchTimeStamp = -1
			lw.headersFetchTimeSpent = -1
			lw.totalDiscoveryTimeSpent = -1

			// begin indexing transactions after sync is completed,
			// syncProgressListeners.OnSynced() will be invoked after transactions are indexed
			lw.IndexTransactions(-1, -1, func() {
				for _, syncProgressListener := range lw.syncProgressListeners {
					if synced {
						syncProgressListener.OnSyncCompleted()
					} else {
						syncProgressListener.OnSyncCanceled()
					}
				}
			})
		},
		FetchMissingCFiltersStarted:  func() {},
		FetchMissingCFiltersProgress: func(missingCFitlersStart, missingCFitlersEnd int32) {},
		FetchMissingCFiltersFinished: func() {},
		FetchHeadersStarted: func() {
			if lw.beginFetchTimeStamp != -1 {
				// already started headers fetching
				return
			}

			lw.beginFetchTimeStamp = time.Now().Unix()
			lw.startHeaderHeight = lw.GetBestBlock()
			lw.totalFetchedHeadersCount = 0

			if lw.showLogs && lw.syncing {
				walletBestBlockTime := lw.GetBestBlockTimeStamp()
				totalHeadersToFetch := lw.estimateBlockHeadersCountAfter(walletBestBlockTime)
				log.Infof("Step 1 of 3 - fetching %d block headers.\n", totalHeadersToFetch)
			}
		},
		FetchHeadersProgress: lw.fetchHeadersProgress,
		FetchHeadersFinished: func() {

			lw.startHeaderHeight = -1
			lw.headersFetchTimeSpent = time.Now().Unix() - lw.beginFetchTimeStamp

			// If there is some period of inactivity reported at this stage,
			// subtract it from the total stage time.
			lw.headersFetchTimeSpent -= lw.totalInactiveSeconds
			lw.totalInactiveSeconds = 0

			if lw.headersFetchTimeSpent < 150 {
				// This ensures that minimum ETA used for stage 2 (address discovery) is 120 seconds (80% of 150 seconds).
				lw.headersFetchTimeSpent = 150
			}

			lw.totalFetchedHeadersCount = 0

			if lw.showLogs && lw.syncing {
				log.Info("Fetch headers completed.")
			}
		},
		DiscoverAddressesStarted: lw.discoverAddressesStarted,
		DiscoverAddressesFinished: func() {

			addressDiscoveryFinishTime := time.Now().Unix()
			lw.totalDiscoveryTimeSpent = addressDiscoveryFinishTime - lw.addressDiscoveryStartTime

			close(lw.addressDiscoveryCompleted)
			lw.addressDiscoveryCompleted = nil

			if !loadedWallet.Locked() {
				loadedWallet.Lock()
			}
		},
		RescanStarted: func() {
			if lw.addressDiscoveryCompleted != nil {
				close(lw.addressDiscoveryCompleted)
				lw.addressDiscoveryCompleted = nil
			}

			lw.rescanStartTime = time.Now().Unix()

			// retain last total progress report from address discovery phase
			lw.headersRescanProgress.TotalTimeRemainingSeconds = lw.addressDiscoveryProgress.TotalTimeRemainingSeconds
			lw.headersRescanProgress.TotalSyncProgress = lw.addressDiscoveryProgress.TotalSyncProgress

			if lw.showLogs && lw.syncing {
				log.Info("Step 3 of 3 - Scanning block headers")
			}
		},
		RescanProgress: lw.rescanProgress,
		RescanFinished: func() {
			lw.publishHeadersRescanProgress()
		},
	}
}

func (lw *LibWallet) handlePeerCountUpdate(peerCount int32) {
	lw.connectedPeers = peerCount
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnPeerConnectedOrDisconnected(peerCount)
	}

	if lw.showLogs && lw.syncing {
		if peerCount == 1 {
			log.Infof("Connected to %d peer on %s.\n", peerCount, lw.activeNet.Name)
		} else {
			log.Infof("Connected to %d peers on %s.\n", peerCount, lw.activeNet.Name)
		}
	}
}

func (lw *LibWallet) notifySyncError(code SyncErrorCode, err error) {
	lw.syncing = false
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnSyncEndedWithError(err)
	}
}

func (lw *LibWallet) notifySyncCanceled() {
	lw.syncing = false
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnSyncCanceled()
	}
}

func (lw *LibWallet) fetchHeadersProgress(fetchedHeadersCount int32, lastHeaderTime int64) {
	if !lw.syncing || lw.headersFetchTimeSpent != -1 {
		// Ignore this call because this function gets called for each peer and
		// we'd want to ignore those calls as far as the wallet is synced (i.e. !syncListener.syncing)
		// or headers are completely fetched (i.e. syncListener.headersFetchTimeSpent != -1)
		return
	}

	// If there was some period of inactivity,
	// assume that this process started at some point in the future,
	// thereby accounting for the total reported time of inactivity.
	lw.beginFetchTimeStamp += lw.totalInactiveSeconds
	lw.totalInactiveSeconds = 0

	lw.totalFetchedHeadersCount += fetchedHeadersCount
	headersLeftToFetch := lw.estimateBlockHeadersCountAfter(lastHeaderTime)
	totalHeadersToFetch := lw.totalFetchedHeadersCount + headersLeftToFetch
	headersFetchProgress := float64(lw.totalFetchedHeadersCount) / float64(totalHeadersToFetch)

	// update headers fetching progress report
	lw.headersFetchProgress.TotalHeadersToFetch = totalHeadersToFetch
	lw.headersFetchProgress.CurrentHeaderTimestamp = lastHeaderTime
	lw.headersFetchProgress.FetchedHeadersCount = lw.totalFetchedHeadersCount
	lw.headersFetchProgress.HeadersFetchProgress = roundUp(headersFetchProgress * 100.0)

	timeTakenSoFar := time.Now().Unix() - lw.beginFetchTimeStamp
	estimatedTotalHeadersFetchTime := float64(timeTakenSoFar) / headersFetchProgress

	estimatedDiscoveryTime := estimatedTotalHeadersFetchTime * DiscoveryPercentage
	estimatedRescanTime := estimatedTotalHeadersFetchTime * RescanPercentage
	estimatedTotalSyncTime := estimatedTotalHeadersFetchTime + estimatedDiscoveryTime + estimatedRescanTime

	// update total progress percentage and total time remaining
	totalSyncProgress := float64(timeTakenSoFar) / estimatedTotalSyncTime
	totalTimeRemainingSeconds := int64(math.Round(estimatedTotalSyncTime)) - timeTakenSoFar
	lw.headersFetchProgress.TotalSyncProgress = roundUp(totalSyncProgress * 100.0)
	lw.headersFetchProgress.TotalTimeRemainingSeconds = totalTimeRemainingSeconds

	// notify progress listener of estimated progress report
	lw.publishFetchHeadersProgress()

	headersFetchTimeRemaining := estimatedTotalHeadersFetchTime - float64(timeTakenSoFar)
	debugInfo := &DebugInfo{
		timeTakenSoFar,
		totalTimeRemainingSeconds,
		timeTakenSoFar,
		int64(math.Round(headersFetchTimeRemaining)),
	}
	lw.publishDebugInfo(debugInfo)
}

func (lw *LibWallet) publishFetchHeadersProgress() {
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnHeadersFetchProgress(&lw.headersFetchProgress)
	}
}

func (lw *LibWallet) discoverAddressesStarted() {
	if lw.addressDiscoveryCompleted != nil {
		return
	}

	lw.addressDiscoveryStartTime = time.Now().Unix()
	if lw.showLogs && lw.syncing {
		log.Info("Step 2 of 3 - discovering used addresses.")
	}

	lw.updateAddressDiscoveryProgress()
}

func (lw *LibWallet) updateAddressDiscoveryProgress() {
	// these values will be used every second to calculate the total sync progress
	totalHeadersFetchTime := float64(lw.headersFetchTimeSpent)
	estimatedDiscoveryTime := totalHeadersFetchTime * DiscoveryPercentage
	estimatedRescanTime := totalHeadersFetchTime * RescanPercentage

	// following channels are used to determine next step in the below subroutine
	everySecondTicker := time.NewTicker(1 * time.Second)
	everySecondTickerChannel := everySecondTicker.C

	// track last logged time remaining and total percent to avoid re-logging same message
	var lastTimeRemaining int64
	var lastTotalPercent int32 = -1

	lw.addressDiscoveryCompleted = make(chan bool)

	go func() {
		for {
			// If there was some period of inactivity,
			// assume that this process started at some point in the future,
			// thereby accounting for the total reported time of inactivity.
			lw.addressDiscoveryStartTime += lw.totalInactiveSeconds
			lw.totalInactiveSeconds = 0

			select {
			case <-everySecondTickerChannel:
				// calculate address discovery progress
				elapsedDiscoveryTime := float64(time.Now().Unix() - lw.addressDiscoveryStartTime)
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
				lw.addressDiscoveryProgress.AddressDiscoveryProgress = int32(math.Round(discoveryProgress))
				lw.addressDiscoveryProgress.TotalSyncProgress = totalProgressPercent
				lw.addressDiscoveryProgress.TotalTimeRemainingSeconds = totalTimeRemainingSeconds

				lw.publishAddressDiscoveryProgress()

				debugInfo := &DebugInfo{
					int64(math.Round(totalElapsedTime)),
					totalTimeRemainingSeconds,
					int64(math.Round(elapsedDiscoveryTime)),
					int64(math.Round(remainingAccountDiscoveryTime)),
				}
				lw.publishDebugInfo(debugInfo)

				if lw.showLogs && lw.syncing {
					// avoid logging same message multiple times
					if totalProgressPercent != lastTotalPercent || totalTimeRemainingSeconds != lastTimeRemaining {
						log.Infof("Syncing %d%%, %s remaining, discovering used addresses.\n",
							totalProgressPercent, calculateTotalTimeRemaining(totalTimeRemainingSeconds))

						lastTotalPercent = totalProgressPercent
						lastTimeRemaining = totalTimeRemainingSeconds
					}
				}

			case <-lw.addressDiscoveryCompleted:
				// stop updating time taken and progress for address discovery
				everySecondTicker.Stop()

				if lw.showLogs && lw.syncing {
					log.Info("Address discovery complete.")
				}

				return
			}
		}
	}()
}

func (lw *LibWallet) publishAddressDiscoveryProgress() {
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnAddressDiscoveryProgress(&lw.addressDiscoveryProgress)
	}
}

func (lw *LibWallet) rescanProgress(rescannedThrough int32) {

	lw.headersRescanProgress.TotalHeadersToScan = lw.GetBestBlock()

	rescanRate := float64(rescannedThrough) / float64(lw.headersRescanProgress.TotalHeadersToScan)
	lw.headersRescanProgress.RescanProgress = int32(math.Round(rescanRate * 100))
	lw.headersRescanProgress.CurrentRescanHeight = rescannedThrough

	// If there was some period of inactivity,
	// assume that this process started at some point in the future,
	// thereby accounting for the total reported time of inactivity.
	lw.rescanStartTime += lw.totalInactiveSeconds
	lw.totalInactiveSeconds = 0

	elapsedRescanTime := time.Now().Unix() - lw.rescanStartTime
	totalElapsedTime := lw.headersFetchTimeSpent + lw.totalDiscoveryTimeSpent + elapsedRescanTime

	estimatedTotalRescanTime := float64(elapsedRescanTime) / rescanRate
	estimatedTotalSyncTime := lw.headersFetchTimeSpent + lw.totalDiscoveryTimeSpent + int64(math.Round(estimatedTotalRescanTime))
	totalProgress := (float64(totalElapsedTime) / float64(estimatedTotalSyncTime)) * 100

	totalTimeRemainingSeconds := int64(math.Round(estimatedTotalRescanTime)) + elapsedRescanTime

	// do not update total time taken and total progress percent if elapsedRescanTime is 0
	// because the estimatedTotalRescanTime will be inaccurate (also 0)
	// which will make the estimatedTotalSyncTime equal to totalElapsedTime
	// giving the wrong impression that the process is complete
	if elapsedRescanTime > 0 {
		lw.headersRescanProgress.TotalTimeRemainingSeconds = totalTimeRemainingSeconds
		lw.headersRescanProgress.TotalSyncProgress = int32(math.Round(totalProgress))
	}

	log.Infof("Rescanned Through %d, Progress: %d, Rate: %f", rescannedThrough, lw.headersRescanProgress.RescanProgress, rescanRate)

	lw.publishHeadersRescanProgress()

	debugInfo := &DebugInfo{
		totalElapsedTime,
		totalTimeRemainingSeconds,
		elapsedRescanTime,
		int64(math.Round(estimatedTotalRescanTime)) - elapsedRescanTime,
	}
	lw.publishDebugInfo(debugInfo)

	if lw.showLogs && lw.syncing {
		log.Infof("Syncing %d%%, %s remaining, scanning %d of %d block headers.\n",
			lw.headersRescanProgress.TotalSyncProgress,
			calculateTotalTimeRemaining(lw.headersRescanProgress.TotalTimeRemainingSeconds),
			lw.headersRescanProgress.CurrentRescanHeight,
			lw.headersRescanProgress.TotalHeadersToScan,
		)
	}
}

func (lw *LibWallet) publishHeadersRescanProgress() {
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnHeadersRescanProgress(&lw.headersRescanProgress)
	}
}

func (lw *LibWallet) publishDebugInfo(debugInfo *DebugInfo) {
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.Debug(debugInfo)
	}
}

/** Helper functions start here */

func (lw *LibWallet) estimateBlockHeadersCountAfter(lastHeaderTime int64) int32 {
	if lastHeaderTime == 0 {
		// use wallet's best block time for estimation
		lastHeaderTime = lw.GetBestBlockTimeStamp()
	}

	// Use the difference between current time (now) and last reported block time, to estimate total headers to fetch
	timeDifference := time.Now().Unix() - lastHeaderTime
	estimatedHeadersDifference := float64(timeDifference) / float64(lw.targetTimePerBlock)

	// return next integer value (upper limit) if estimatedHeadersDifference is a fraction
	return int32(math.Ceil(estimatedHeadersDifference))
}
