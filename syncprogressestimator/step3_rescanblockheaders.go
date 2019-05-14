package syncprogressestimator

import (
	"fmt"
	"math"
	"time"
)

func (syncListener *SyncProgressEstimator) OnRescan(rescannedThrough int32, state string) {
	if syncListener.addressDiscoveryCompleted != nil {
		close(syncListener.addressDiscoveryCompleted)
		syncListener.addressDiscoveryCompleted = nil
	}

	syncListener.headersRescanProgress.TotalHeadersToScan = syncListener.getBestBlock()

	switch state {
	case SyncStateStart:
		syncListener.rescanStartTime = time.Now().Unix()

		if syncListener.showLog {
			fmt.Println("Step 3 of 3 - Scanning block headers")
		}

	case SyncStateProgress:
		rescanRate := float64(rescannedThrough) / float64(syncListener.headersRescanProgress.TotalHeadersToScan)
		syncListener.headersRescanProgress.RescanProgress = int32(math.Round(rescanRate * 100))
		syncListener.headersRescanProgress.CurrentRescanHeight = rescannedThrough

		elapsedRescanTime := time.Now().Unix() - syncListener.rescanStartTime
		totalElapsedTime := syncListener.headersFetchTimeSpent + syncListener.totalDiscoveryTimeSpent + elapsedRescanTime

		// do not update total time taken and total progress percent if elapsedRescanTime is 0
		// because the estimatedTotalRescanTime will be inaccurate (also 0)
		// which will make the estimatedTotalSyncTime equal to totalElapsedTime
		// giving the wrong impression that the process is complete
		if elapsedRescanTime > 0 {
			estimatedTotalRescanTime := float64(elapsedRescanTime) / rescanRate
			estimatedTotalSyncTime := syncListener.headersFetchTimeSpent + syncListener.totalDiscoveryTimeSpent +
				int64(math.Round(estimatedTotalRescanTime))
			totalProgress := (float64(totalElapsedTime) / float64(estimatedTotalSyncTime)) * 100

			totalTimeRemainingSeconds := int64(math.Round(estimatedTotalRescanTime)) + elapsedRescanTime
			syncListener.generalProgress.TotalTimeRemainingSeconds = totalTimeRemainingSeconds
			syncListener.generalProgress.TotalSyncProgress = int32(math.Round(totalProgress))
		}

		if syncListener.showLog {
			fmt.Printf("Syncing %d%%, %s remaining, scanning %d of %d block headers.\n",
				syncListener.generalProgress.TotalSyncProgress,
				calculateTotalTimeRemaining(syncListener.generalProgress.TotalTimeRemainingSeconds),
				syncListener.headersRescanProgress.CurrentRescanHeight,
				syncListener.headersRescanProgress.TotalHeadersToScan,
			)
		}

	case SyncStateFinish:
		if syncListener.showLog {
			fmt.Println("Block headers scan complete.")
		}
	}

	syncListener.progressListener.OnHeadersRescanProgress(syncListener.headersRescanProgress, syncListener.generalProgress)
}
