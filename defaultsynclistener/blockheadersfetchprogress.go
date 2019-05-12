package defaultsynclistener

import (
	"fmt"
	"time"

	"github.com/raedahgroup/dcrlibwallet"
	"math"
)

func (syncListener *DefaultSyncListener) OnFetchedHeaders(fetchedHeadersCount int32, lastHeaderTime int64, state string) {
	if !syncListener.syncing || syncListener.headersFetchTimeSpent != -1 {
		// Ignore this call because this function gets called for each peer and
		// we'd want to ignore those calls as far as the wallet is synced (i.e. !syncListener.syncing)
		// or headers are completely fetched (i.e. syncListener.headersFetchTimeSpent != -1)
		return
	}

	bestBlockTimeStamp := syncListener.getBestBlockTimestamp()
	bestBlock := syncListener.getBestBlock()
	estimatedFinalBlockHeight := estimateFinalBlockHeight(syncListener.netType, bestBlockTimeStamp, bestBlock)

	switch state {
	case dcrlibwallet.SyncStateStart:
		if syncListener.beginFetchTimeStamp != -1 {
			// already started headers fetching
			break
		}

		syncListener.beginFetchTimeStamp = time.Now().Unix()
		syncListener.startHeaderHeight = bestBlock
		syncListener.currentHeaderHeight = syncListener.startHeaderHeight

		totalHeadersToFetch := int32(estimatedFinalBlockHeight) - syncListener.startHeaderHeight

		syncListener.progressReport.Update(SyncStatusInProgress, func(report *progressReport) {
			report.CurrentStep = FetchingBlockHeaders
			report.TotalHeadersToFetch = totalHeadersToFetch
		})

		if syncListener.showLog {
			fmt.Printf("Step 1 of 3 - fetching %d block headers.\n", totalHeadersToFetch)
		}

	case dcrlibwallet.SyncStateProgress:
		// increment current block height value
		syncListener.currentHeaderHeight += fetchedHeadersCount

		// calculate percentage progress and eta
		totalFetchedHeaders := syncListener.currentHeaderHeight
		if syncListener.startHeaderHeight > 0 {
			totalFetchedHeaders -= syncListener.startHeaderHeight
		}

		syncEndPoint := estimatedFinalBlockHeight - syncListener.startHeaderHeight
		headersFetchingRate := float64(totalFetchedHeaders) / float64(syncEndPoint)

		timeTakenSoFar := time.Now().Unix() - syncListener.beginFetchTimeStamp
		estimatedTotalHeadersFetchTime := math.Round(float64(timeTakenSoFar) / headersFetchingRate)

		estimatedRescanTime := math.Round(estimatedTotalHeadersFetchTime * RescanPercentage)
		estimatedDiscoveryTime := math.Round(estimatedTotalHeadersFetchTime * DiscoveryPercentage)
		estimatedTotalSyncTime := estimatedTotalHeadersFetchTime + estimatedRescanTime + estimatedDiscoveryTime

		totalTimeRemaining := estimatedTotalSyncTime - float64(timeTakenSoFar)
		totalSyncProgress := (float64(timeTakenSoFar) / float64(estimatedTotalSyncTime)) * 100.0

		syncListener.progressReport.Update(SyncStatusInProgress, func(report *progressReport) {
			// update total progress info
			report.TotalSyncProgress = int32(math.Round(totalSyncProgress))
			report.TotalTimeRemaining = calculateTotalTimeRemaining(totalTimeRemaining)

			// update headers statistics
			report.TotalHeadersToFetch = syncEndPoint
			report.DaysBehind = calculateDaysBehind(lastHeaderTime)

			// update headers progress sync info
			report.FetchedHeadersCount = totalFetchedHeaders
			report.HeadersFetchProgress = int32(math.Round(headersFetchingRate * 100))
		})

		if syncListener.showLog {
			progressReport := syncListener.progressReport.Read()
			fmt.Printf("Syncing %d%%, %s remaining, fetched %d of %d block headers, %s behind.\n",
				progressReport.TotalSyncProgress, progressReport.TotalTimeRemaining,
				progressReport.FetchedHeadersCount, progressReport.TotalHeadersToFetch,
				progressReport.DaysBehind)
		}

	case dcrlibwallet.SyncStateFinish:
		syncListener.headersFetchTimeSpent = time.Now().Unix() - syncListener.beginFetchTimeStamp
		syncListener.startHeaderHeight = -1
		syncListener.currentHeaderHeight = -1

		if syncListener.showLog {
			fmt.Println("Fetch headers completed.")
		}
	}

	syncListener.syncProgressUpdated(syncListener.progressReport, CurrentStepUpdate)

	if state == dcrlibwallet.SyncStateFinish {
		// clear total headers count to be re-set on RescanHeaders
		syncListener.progressReport.Update(SyncStatusInProgress, func(report *progressReport) {
			report.TotalHeadersToFetch = -1
		})
	}
}
