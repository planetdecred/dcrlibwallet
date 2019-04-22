package defaultsynclistener

import (
	"fmt"
	"github.com/raedahgroup/dcrlibwallet"
)

type DefaultSyncListener struct {
	netType               string
	getBestBlock          func() int32
	getBestBlockTimestamp func() int64

	showLog             bool
	syncing             bool
	progressReport      *ProgressReport
	syncProgressUpdated func(*ProgressReport, SyncOp)

	beginFetchTimeStamp   int64
	startHeaderHeight     int32
	currentHeaderHeight   int32
	headersFetchTimeSpent int64

	addressDiscoveryCompleted chan bool
	totalDiscoveryTimeSpent   int64

	rescanStartTime int64
}

// DefaultSyncProgressListener implements `dcrlibwallet.SyncProgressListener` to receive updates during a sync operation.
// The data received via the different `dcrlibwallet.SyncProgressListener` interface methods are used to
// estimate the progress of the current step of the sync operation and the overall sync progress.
// This estimated progress report is made available to the sync initiator via the `syncProgressUpdated` function.
// If `showLog` is set to true, DefaultSyncProgressListener also prints calculated progress report to stdout.
func DefaultSyncProgressListener(netType string, showLog bool, getBestBlock func() int32, getBestBlockTimestamp func() int64,
	syncInfoUpdated func(*ProgressReport, SyncOp)) *DefaultSyncListener {

	return &DefaultSyncListener{
		netType:               netType,
		getBestBlock:          getBestBlock,
		getBestBlockTimestamp: getBestBlockTimestamp,

		showLog:             showLog,
		syncing:             true,
		progressReport:      InitProgressReport(),
		syncProgressUpdated: syncInfoUpdated,

		beginFetchTimeStamp:   -1,
		headersFetchTimeSpent: -1,

		totalDiscoveryTimeSpent: -1,
	}
}

// Following methods are to satisfy the `dcrlibwallet.SyncProgressListener` interface.
// Other interface methods are implemented in the different *progress.go files in this package.
func (syncListener *DefaultSyncListener) OnFetchMissingCFilters(missingCFiltersStart, missingCFiltersEnd int32, state string) {
}

func (syncListener *DefaultSyncListener) OnIndexTransactions(totalIndexed int32) {
	if syncListener.showLog {
		fmt.Printf("Indexing transactions. %d done.\n", totalIndexed)
	}
}

func (syncListener *DefaultSyncListener) OnSynced(synced bool) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncListener.syncing = false
	syncListener.showLog = false // stop showing logs after sync completes

	if synced {
		syncListener.progressReport.Update(SyncStatusSuccess, func(report *progressReport) {
			report.Done = true
		})
	} else {
		syncListener.progressReport.Update(SyncStatusError, func(report *progressReport) {
			report.Done = true
			report.Error = "Sync failed or canceled"
		})
	}

	// notify sync initiator of update
	syncListener.syncProgressUpdated(syncListener.progressReport, SyncDone)
}

// todo sync may not have ended
func (syncListener *DefaultSyncListener) OnSyncError(code dcrlibwallet.SyncErrorCode, err error) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncListener.syncing = false
	syncListener.showLog = false // stop showing logs after sync completes
	syncListener.progressReport.Update(SyncStatusError, func(report *progressReport) {
		report.Done = true
		report.Error = fmt.Sprintf("Code: %d, Error: %s", code, err.Error())
	})

	// notify sync initiator of update
	syncListener.syncProgressUpdated(syncListener.progressReport, SyncDone)
}
