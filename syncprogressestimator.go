package dcrlibwallet

import "fmt"

type SyncProgressEstimator struct {
	netType               string
	getBestBlock          func() int32
	getBestBlockTimestamp func() int64

	showLog             bool
	syncing             bool

	generalProgress          GeneralSyncProgressReport
	headersFetchProgress     HeadersFetchProgressReport
	addressDiscoveryProgress AddressDiscoveryProgressReport
	headersRescanProgress    HeadersRescanProgressReport

	progressListener EstimatedSyncProgressListener

	beginFetchTimeStamp   int64
	startHeaderHeight     int32
	currentHeaderHeight   int32
	headersFetchTimeSpent int64

	addressDiscoveryCompleted chan bool
	totalDiscoveryTimeSpent   int64

	rescanStartTime int64
}

// SetupSyncProgressEstimator creates an instance of `SyncProgressEstimator` which implements `SyncProgressListener`.
// The created instance can be registered with `AddSyncProgressCallback` to receive updates during a sync operation.
// The data received via the different `SyncProgressListener` interface methods are used to
// estimate the progress of the current step of the sync operation and the overall sync progress.
// This estimated progress report is made available to the sync initiator via the specified `progressListener` callback.
// If `showLog` is set to true, SyncProgressEstimator also prints calculated progress report to stdout.
func SetupSyncProgressEstimator(netType string, showLog bool, getBestBlock func() int32, getBestBlockTimestamp func() int64,
	progressListener EstimatedSyncProgressListener) *SyncProgressEstimator {

	return &SyncProgressEstimator{
		netType:               netType,
		getBestBlock:          getBestBlock,
		getBestBlockTimestamp: getBestBlockTimestamp,

		showLog:             showLog,
		syncing:             true,

		progressListener:   progressListener,

		beginFetchTimeStamp:   -1,
		headersFetchTimeSpent: -1,

		totalDiscoveryTimeSpent: -1,
	}
}

/**
Following methods satisfy the `SyncProgressListener` interface.
Other interface methods are implemented in the different sync***progress.go files in this package.
*/
func (syncListener *SyncProgressEstimator) OnFetchMissingCFilters(missingCFiltersStart, missingCFiltersEnd int32, state string) {
}

func (syncListener *SyncProgressEstimator) OnIndexTransactions(totalIndexed int32) {
	if syncListener.showLog {
		fmt.Printf("Indexing transactions. %d done.\n", totalIndexed)
	}
}

func (syncListener *SyncProgressEstimator) OnSynced(synced bool) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncListener.syncing = false
	syncListener.showLog = false // stop showing logs after sync completes

	if synced {
		syncListener.generalProgress.Done = true
	} else {
		syncListener.generalProgress.Done = true
		syncListener.generalProgress.Error = "Sync failed or canceled"
	}

	// notify sync initiator of update
	syncListener.progressListener.OnGeneralSyncProgress(syncListener.generalProgress)
}

// todo sync may not have ended
func (syncListener *SyncProgressEstimator) OnSyncError(code int32, err error) {
	if !syncListener.syncing {
		// ignore subsequent updates
		return
	}

	syncListener.syncing = false
	syncListener.showLog = false // stop showing logs after sync completes

	syncListener.generalProgress.Done = true
	syncListener.generalProgress.Error = fmt.Sprintf("Code: %d, Error: %s", code, err.Error())

	// notify sync initiator of update
	syncListener.progressListener.OnGeneralSyncProgress(syncListener.generalProgress)
}

