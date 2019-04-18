package defaultsynclistener

import (
	"fmt"
	"github.com/raedahgroup/dcrlibwallet"
)

type defaultSyncListener struct {
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
	syncInfoUpdated func(*ProgressReport, SyncOp)) *defaultSyncListener {

	return &defaultSyncListener{
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
func (syncListener *defaultSyncListener) OnFetchMissingCFilters(missingCFiltersStart, missingCFiltersEnd int32, state string) {
}

func (syncListener *defaultSyncListener) OnIndexTransactions(totalIndex int32) {}

func (syncListener *defaultSyncListener) OnSynced(synced bool) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncListener.syncing = false
	syncListener.showLog = false // stop showing logs after sync completes
	syncListener.progressReport.Update(func(report *progressReport) {
		report.Done = true
		if !synced {
			report.Error = "Sync failed or canceled"
			report.Status = SyncStatusError
		} else {
			report.Status = SyncStatusSuccess
		}
	})

	// notify sync initiator of update
	syncListener.syncProgressUpdated(syncListener.progressReport, SyncDone)
}

// todo sync may not have ended
func (syncListener *defaultSyncListener) OnSyncError(code dcrlibwallet.SyncErrorCode, err error) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncListener.syncing = false
	syncListener.showLog = false // stop showing logs after sync completes
	syncListener.progressReport.Update(func(report *progressReport) {
		report.Error = fmt.Sprintf("Code: %d, Error: %s", code, err.Error())
		report.Status = SyncStatusError
	})

	// notify sync initiator of update
	syncListener.syncProgressUpdated(syncListener.progressReport, SyncDone)
}
