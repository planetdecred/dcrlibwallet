package defaultsynclistener

import (
	"fmt"
	"math"
	"time"

	"github.com/raedahgroup/dcrlibwallet"
)

func (syncListener *defaultSyncListener) OnRescan(rescannedThrough int32, state string) {
	if syncListener.addressDiscoveryCompleted != nil {
		close(syncListener.addressDiscoveryCompleted)
		syncListener.addressDiscoveryCompleted = nil
	}

	if syncListener.progressReport.Read().TotalHeadersToFetch == -1 {
		syncListener.progressReport.Update(SyncStatusInProgress, func(report *progressReport) {
			report.TotalHeadersToFetch = syncListener.getBestBlock()
		})
	}

	switch state {
	case dcrlibwallet.SyncStateStart:
		syncListener.rescanStartTime = time.Now().Unix()

		syncListener.progressReport.Update(SyncStatusInProgress, func(report *progressReport) {
			report.TotalHeadersToFetch = syncListener.getBestBlock()
			report.CurrentStep = ScanningBlockHeaders
		})

		if syncListener.showLog {
			fmt.Println("Step 3 of 3 - Scanning block headers")
		}

	case dcrlibwallet.SyncStateProgress:
		elapsedRescanTime := time.Now().Unix() - syncListener.rescanStartTime
		totalElapsedTime := syncListener.headersFetchTimeSpent + syncListener.totalDiscoveryTimeSpent + elapsedRescanTime

		rescanRate := float64(rescannedThrough) / float64(syncListener.progressReport.Read().TotalHeadersToFetch)
		estimatedTotalRescanTime := float64(elapsedRescanTime) / rescanRate
		estimatedTotalSyncTime := syncListener.headersFetchTimeSpent + syncListener.totalDiscoveryTimeSpent +
			int64(math.Round(estimatedTotalRescanTime))

		totalProgress := (float64(totalElapsedTime) / float64(estimatedTotalSyncTime)) * 100

		// do not update total time taken and total progress percent if elapsedRescanTime is 0
		// because the estimatedTotalRescanTime will be inaccurate (also 0)
		// which will make the estimatedTotalSyncTime equal to totalElapsedTime
		// giving the wrong impression that the process is complete
		if elapsedRescanTime > 0 {
			syncListener.progressReport.Update(SyncStatusInProgress, func(report *progressReport) {
				report.TotalTimeRemaining = calculateTotalTimeRemaining(estimatedTotalRescanTime - float64(elapsedRescanTime))
				report.TotalSyncProgress = int32(math.Round(totalProgress))
				report.RescanProgress = int32(math.Round(rescanRate * 100))
				report.CurrentRescanHeight = rescannedThrough
			})
		} else {
			syncListener.progressReport.Update(SyncStatusInProgress, func(report *progressReport) {
				report.RescanProgress = int32(math.Round(rescanRate * 100))
				report.CurrentRescanHeight = rescannedThrough
			})
		}

		if syncListener.showLog {
			report := syncListener.progressReport.Read()
			fmt.Printf("Syncing %d%%, %s remaining, scanning %d of %d block headers.\n",
				report.TotalSyncProgress, report.TotalTimeRemaining,
				report.CurrentRescanHeight, report.TotalHeadersToFetch)
		}

	case dcrlibwallet.SyncStateFinish:
		if syncListener.showLog {
			fmt.Println("Block headers scan complete.")
		}
	}

	syncListener.syncProgressUpdated(syncListener.progressReport, CurrentStepUpdate)
}
