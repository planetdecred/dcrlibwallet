package blockchainsync

import (
	"fmt"
	"time"
	"math"

	"github.com/decred/dcrwallet/netparams"
)

const (
	PeersCountUpdate  = "peers"
	CurrentStepUpdate = "current step"
	SyncDone          = "done"
)

type defaultSyncListener struct {
	activeNet             *netparams.Params
	getBestBlock          func() int32
	getBestBlockTimestamp func() int64

	netType         string
	showLog         bool
	syncing         bool
	syncInfoUpdated func(*PrivateSyncInfo, string)

	privateSyncInfo *PrivateSyncInfo
	headersData     *FetchHeadersData

	addressDiscoveryCompleted chan bool

	rescanStartTime int64
}

func DefaultSyncProgressListener(activeNet *netparams.Params, showLog bool, getBestBlock func() int32, getBestBlockTimestamp func() int64,
	syncInfoUpdated func(*PrivateSyncInfo, string)) *defaultSyncListener {

	return &defaultSyncListener{
		activeNet: activeNet,
		getBestBlock: getBestBlock,
		getBestBlockTimestamp: getBestBlockTimestamp,

		netType:         activeNet.Params.Name,
		showLog:         showLog,
		syncing:         true,
		syncInfoUpdated: syncInfoUpdated,

		privateSyncInfo: NewPrivateInfo(),
		headersData: &FetchHeadersData{
			BeginFetchTimeStamp: -1,
		},
	}
}

// following functions are used to implement ProgressListener interface
func (syncListener *defaultSyncListener) OnPeerConnected(peerCount int32) {
	syncInfo := syncListener.privateSyncInfo.Read()
	syncInfo.ConnectedPeers = peerCount
	syncListener.privateSyncInfo.Write(syncInfo, syncInfo.Status)

	// notify interface of update
	syncListener.syncInfoUpdated(syncListener.privateSyncInfo, PeersCountUpdate)

	if syncListener.showLog && syncListener.syncing {
		if peerCount == 1 {
			fmt.Printf("Connected to %d peer on %s.\n", peerCount, syncListener.netType)
		} else {
			fmt.Printf("Connected to %d peers on %s.\n", peerCount, syncListener.netType)
		}
	}
}

func (syncListener *defaultSyncListener) OnPeerDisconnected(peerCount int32) {
	syncInfo := syncListener.privateSyncInfo.Read()
	syncInfo.ConnectedPeers = peerCount
	syncListener.privateSyncInfo.Write(syncInfo, syncInfo.Status)

	// notify interface of update
	syncListener.syncInfoUpdated(syncListener.privateSyncInfo, PeersCountUpdate)

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
	syncInfo := syncListener.privateSyncInfo.Read()

	if !syncListener.syncing || syncInfo.HeadersFetchTimeTaken != -1 {
		// Ignore this call because this function gets called for each peer and
		// we'd want to ignore those calls as far as the wallet is synced (i.e. !syncListener.syncing)
		// or headers are completely fetched (i.e. syncInfo.HeadersFetchTimeTaken != -1)
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

		syncInfo.TotalHeadersToFetch = int32(estimatedFinalBlockHeight) - syncListener.headersData.StartHeaderHeight
		syncInfo.CurrentStep = 1

		if syncListener.showLog {
			fmt.Printf("Step 1 of 3 - fetching %d block headers.\n", syncInfo.TotalHeadersToFetch)
		}

	case PROGRESS:
		headersFetchReport := FetchHeadersProgressReport{
			FetchedHeadersCount:       fetchedHeadersCount,
			LastHeaderTime:            lastHeaderTime,
			EstimatedFinalBlockHeight: estimatedFinalBlockHeight,
		}
		updateFetchHeadersProgress(syncInfo, syncListener.headersData, headersFetchReport)

		if syncListener.showLog {
			fmt.Printf("Syncing %d%%, %s remaining, fetched %d of %d block headers, %s behind.\n",
				syncInfo.TotalSyncProgress, syncInfo.TotalTimeRemaining,
				syncInfo.FetchedHeadersCount, syncInfo.TotalHeadersToFetch,
				syncInfo.DaysBehind)
		}

	case FINISH:
		syncInfo.HeadersFetchTimeTaken = time.Now().Unix() - syncListener.headersData.BeginFetchTimeStamp
		syncInfo.TotalHeadersToFetch = -1

		syncListener.headersData.StartHeaderHeight = -1
		syncListener.headersData.CurrentHeaderHeight = -1

		if syncListener.showLog {
			fmt.Println("Fetch headers completed.")
		}
	}

	syncListener.privateSyncInfo.Write(syncInfo, StatusInProgress)

	// notify ui of updated sync info
	syncListener.syncInfoUpdated(syncListener.privateSyncInfo, CurrentStepUpdate)
}

func (syncListener *defaultSyncListener) OnDiscoveredAddresses(state string) {
	if state == START && syncListener.addressDiscoveryCompleted == nil {
		if syncListener.showLog && syncListener.syncing {
			fmt.Println("Step 2 of 3 - discovering used addresses.")
		}
		syncListener.addressDiscoveryCompleted = updateAddressDiscoveryProgress(syncListener.privateSyncInfo, syncListener.showLog,
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

	syncInfo := syncListener.privateSyncInfo.Read()

	if syncInfo.TotalHeadersToFetch == -1 {
		syncInfo.TotalHeadersToFetch = syncListener.getBestBlock()
	}

	switch state {
	case START:
		syncListener.rescanStartTime = time.Now().Unix()
		syncInfo.TotalHeadersToFetch = syncListener.getBestBlock()
		syncInfo.CurrentStep = 3

		if syncListener.showLog && syncListener.syncing {
			fmt.Println("Step 3 of 3 - Rescanning blocks")
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

	syncListener.privateSyncInfo.Write(syncInfo, StatusInProgress)

	// notify ui of updated sync info
	syncListener.syncInfoUpdated(syncListener.privateSyncInfo, CurrentStepUpdate)
}

func (syncListener *defaultSyncListener) OnIndexTransactions(totalIndex int32) {}

func (syncListener *defaultSyncListener) OnSynced(synced bool) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncInfo := syncListener.privateSyncInfo.Read()
	syncInfo.Done = true
	syncListener.syncing = false

	if !synced {
		syncInfo.Error = "Sync failed or canceled"
		syncListener.privateSyncInfo.Write(syncInfo, StatusError)
	} else {
		syncListener.privateSyncInfo.Write(syncInfo, StatusSuccess)
	}

	// notify interface of update
	syncListener.syncInfoUpdated(syncListener.privateSyncInfo, SyncDone)
}

// todo sync may not have ended
func (syncListener *defaultSyncListener) OnSyncError(code ErrorCode, err error) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncInfo := syncListener.privateSyncInfo.Read()
	syncInfo.Done = true
	syncListener.syncing = false

	syncInfo.Error = fmt.Sprintf("Code: %d, Error: %s", code, err.Error())
	syncListener.privateSyncInfo.Write(syncInfo, StatusError)

	// notify interface of update
	syncListener.syncInfoUpdated(syncListener.privateSyncInfo, SyncDone)
}

