package blockchainsync

import (
	"fmt"
	"math"
	"time"

	"github.com/decred/dcrwallet/netparams"
)

type Op uint8

const (
	PeersCountUpdate Op = iota
	CurrentStepUpdate
	SyncDone
)

type defaultSyncListener struct {
	activeNet             *netparams.Params
	getBestBlock          func() int32
	getBestBlockTimestamp func() int64

	netType         string
	showLog         bool
	syncing         bool
	syncInfoUpdated func(*SyncInfo, Op)

	syncInfo    *SyncInfo
	headersData *FetchHeadersData

	addressDiscoveryCompleted chan bool

	rescanStartTime int64
}

func DefaultSyncProgressListener(activeNet *netparams.Params, showLog bool, getBestBlock func() int32, getBestBlockTimestamp func() int64,
	syncInfoUpdated func(*SyncInfo, Op)) *defaultSyncListener {

	return &defaultSyncListener{
		activeNet:             activeNet,
		getBestBlock:          getBestBlock,
		getBestBlockTimestamp: getBestBlockTimestamp,

		netType:         activeNet.Params.Name,
		showLog:         showLog,
		syncing:         true,
		syncInfoUpdated: syncInfoUpdated,

		syncInfo: InitSyncInfo(),
		headersData: &FetchHeadersData{
			BeginFetchTimeStamp: -1,
		},
	}
}

// following functions are used to implement ProgressListener interface
func (syncListener *defaultSyncListener) OnPeerConnected(peerCount int32) {
	syncInfo := syncListener.syncInfo.Read()
	syncInfo.ConnectedPeers = peerCount
	syncListener.syncInfo.Write(syncInfo, syncInfo.Status)

	// notify interface of update
	syncListener.syncInfoUpdated(syncListener.syncInfo, PeersCountUpdate)

	if syncListener.showLog && syncListener.syncing {
		if peerCount == 1 {
			fmt.Printf("Connected to %d peer on %s.\n", peerCount, syncListener.netType)
		} else {
			fmt.Printf("Connected to %d peers on %s.\n", peerCount, syncListener.netType)
		}
	}
}

func (syncListener *defaultSyncListener) OnPeerDisconnected(peerCount int32) {
	syncInfo := syncListener.syncInfo.Read()
	syncInfo.ConnectedPeers = peerCount
	syncListener.syncInfo.Write(syncInfo, syncInfo.Status)

	// notify interface of update
	syncListener.syncInfoUpdated(syncListener.syncInfo, PeersCountUpdate)

	if syncListener.showLog && syncListener.syncing {
		if peerCount == 1 {
			fmt.Printf("Connected to %d peer on %s.\n", peerCount, syncListener.netType)
		} else {
			fmt.Printf("Connected to %d peers on %s.\n", peerCount, syncListener.netType)
		}
	}
}

func (syncListener *defaultSyncListener) OnFetchMissingCFilters(missingCFiltersStart, missingCFiltersEnd int32, state string) {
}

func (syncListener *defaultSyncListener) OnFetchedHeaders(fetchedHeadersCount int32, lastHeaderTime int64, state string) {
	readableSyncInfo := syncListener.syncInfo.Read()

	if !syncListener.syncing || readableSyncInfo.HeadersFetchTimeTaken != -1 {
		// Ignore this call because this function gets called for each peer and
		// we'd want to ignore those calls as far as the wallet is synced (i.e. !syncListener.syncing)
		// or headers are completely fetched (i.e. readableSyncInfo.HeadersFetchTimeTaken != -1)
		return
	}

	bestBlockTimeStamp := syncListener.getBestBlockTimestamp()
	bestBlock := syncListener.getBestBlock()
	estimatedFinalBlockHeight := estimateFinalBlockHeight(syncListener.netType, bestBlockTimeStamp, bestBlock)

	switch state {
	case START:
		if syncListener.headersData.BeginFetchTimeStamp != -1 {
			break
		}

		syncListener.headersData.BeginFetchTimeStamp = time.Now().Unix()
		syncListener.headersData.StartHeaderHeight = bestBlock
		syncListener.headersData.CurrentHeaderHeight = syncListener.headersData.StartHeaderHeight

		readableSyncInfo.TotalHeadersToFetch = int32(estimatedFinalBlockHeight) - syncListener.headersData.StartHeaderHeight
		readableSyncInfo.CurrentStep = FetchingBlockHeaders

		if syncListener.showLog {
			fmt.Printf("Step 1 of 3 - fetching %d block headers.\n", readableSyncInfo.TotalHeadersToFetch)
		}

	case PROGRESS:
		headersFetchReport := FetchHeadersProgressReport{
			FetchedHeadersCount:       fetchedHeadersCount,
			LastHeaderTime:            lastHeaderTime,
			EstimatedFinalBlockHeight: estimatedFinalBlockHeight,
		}
		updateFetchHeadersProgress(readableSyncInfo, syncListener.headersData, headersFetchReport)

		if syncListener.showLog {
			fmt.Printf("Syncing %d%%, %s remaining, fetched %d of %d block headers, %s behind.\n",
				readableSyncInfo.TotalSyncProgress, readableSyncInfo.TotalTimeRemaining,
				readableSyncInfo.FetchedHeadersCount, readableSyncInfo.TotalHeadersToFetch,
				readableSyncInfo.DaysBehind)
		}

	case FINISH:
		readableSyncInfo.HeadersFetchTimeTaken = time.Now().Unix() - syncListener.headersData.BeginFetchTimeStamp
		readableSyncInfo.TotalHeadersToFetch = -1

		syncListener.headersData.StartHeaderHeight = -1
		syncListener.headersData.CurrentHeaderHeight = -1

		if syncListener.showLog {
			fmt.Println("Fetch headers completed.")
		}
	}

	syncListener.syncInfo.Write(readableSyncInfo, StatusInProgress)

	// notify ui of updated sync info
	syncListener.syncInfoUpdated(syncListener.syncInfo, CurrentStepUpdate)
}

func (syncListener *defaultSyncListener) OnDiscoveredAddresses(state string) {
	if state == START && syncListener.addressDiscoveryCompleted == nil {
		if syncListener.showLog && syncListener.syncing {
			fmt.Println("Step 2 of 3 - discovering used addresses.")
		}
		syncListener.addressDiscoveryCompleted = updateAddressDiscoveryProgress(syncListener.syncInfo, syncListener.showLog,
			syncListener.syncInfoUpdated)
	} else {
		close(syncListener.addressDiscoveryCompleted)
		syncListener.addressDiscoveryCompleted = nil
	}
}

func (syncListener *defaultSyncListener) OnRescan(rescannedThrough int32, state string) {
	if syncListener.addressDiscoveryCompleted != nil {
		close(syncListener.addressDiscoveryCompleted)
		syncListener.addressDiscoveryCompleted = nil
	}

	syncInfo := syncListener.syncInfo.Read()

	if syncInfo.TotalHeadersToFetch == -1 {
		syncInfo.TotalHeadersToFetch = syncListener.getBestBlock()
	}

	switch state {
	case START:
		syncListener.rescanStartTime = time.Now().Unix()
		syncInfo.TotalHeadersToFetch = syncListener.getBestBlock()
		syncInfo.CurrentStep = ScanningBlockHeaders

		if syncListener.showLog && syncListener.syncing {
			fmt.Println("Step 3 of 3 - Scanning block headers")
		}

	case PROGRESS:
		elapsedRescanTime := time.Now().Unix() - syncListener.rescanStartTime
		totalElapsedTime := syncInfo.HeadersFetchTimeTaken + syncInfo.TotalDiscoveryTime + elapsedRescanTime

		rescanRate := float64(rescannedThrough) / float64(syncInfo.TotalHeadersToFetch)
		estimatedTotalRescanTime := float64(elapsedRescanTime) / rescanRate
		estimatedTotalSyncTime := syncInfo.HeadersFetchTimeTaken + syncInfo.TotalDiscoveryTime + int64(math.Round(estimatedTotalRescanTime))

		totalProgress := (float64(totalElapsedTime) / float64(estimatedTotalSyncTime)) * 100

		// do not update total time taken and total progress percent if elapsedRescanTime is 0
		// because the estimatedTotalRescanTime will be inaccurate (also 0)
		// which will make the estimatedTotalSyncTime equal to totalElapsedTime
		// giving the wrong impression that the process is complete
		if elapsedRescanTime > 0 {
			syncInfo.TotalTimeRemaining = calculateTotalTimeRemaining(estimatedTotalRescanTime - float64(elapsedRescanTime))
			syncInfo.TotalSyncProgress = int32(math.Round(totalProgress))
		}

		syncInfo.RescanProgress = int32(math.Round(rescanRate * 100))
		syncInfo.CurrentRescanHeight = rescannedThrough

		if syncListener.showLog && syncListener.syncing {
			fmt.Printf("Syncing %d%%, %s remaining, scanning %d of %d block headers.\n",
				syncInfo.TotalSyncProgress, syncInfo.TotalTimeRemaining,
				syncInfo.CurrentRescanHeight, syncInfo.TotalHeadersToFetch)
		}

	case FINISH:
		if syncListener.showLog && syncListener.syncing {
			fmt.Println("Block headers scan complete.")
		}
	}

	syncListener.syncInfo.Write(syncInfo, StatusInProgress)

	// notify ui of updated sync info
	syncListener.syncInfoUpdated(syncListener.syncInfo, CurrentStepUpdate)
}

func (syncListener *defaultSyncListener) OnIndexTransactions(totalIndex int32) {}

func (syncListener *defaultSyncListener) OnSynced(synced bool) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncInfo := syncListener.syncInfo.Read()
	syncInfo.Done = true
	syncListener.syncing = false

	if !synced {
		syncInfo.Error = "Sync failed or canceled"
		syncListener.syncInfo.Write(syncInfo, StatusError)
	} else {
		syncListener.syncInfo.Write(syncInfo, StatusSuccess)
	}

	// notify interface of update
	syncListener.syncInfoUpdated(syncListener.syncInfo, SyncDone)
}

// todo sync may not have ended
func (syncListener *defaultSyncListener) OnSyncError(code ErrorCode, err error) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncInfo := syncListener.syncInfo.Read()
	syncInfo.Done = true
	syncListener.syncing = false

	syncInfo.Error = fmt.Sprintf("Code: %d, Error: %s", code, err.Error())
	syncListener.syncInfo.Write(syncInfo, StatusError)

	// notify interface of update
	syncListener.syncInfoUpdated(syncListener.syncInfo, SyncDone)
}
