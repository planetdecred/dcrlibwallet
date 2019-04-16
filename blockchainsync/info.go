package blockchainsync

import "sync"

type SyncStep uint8

const (
	FetchingBlockHeaders SyncStep = iota
	DiscoveringUsedAddresses
	ScanningBlockHeaders
)

// SyncInfo holds information about a sync op in private variables
// to prevent reading/writing the values directly during a sync op.
type SyncInfo struct {
	sync.RWMutex

	status         Status
	connectedPeers int32
	error          string
	done           bool

	currentStep        SyncStep
	totalSyncProgress  int32
	totalTimeRemaining string

	totalHeadersToFetch   int32
	daysBehind            string
	fetchedHeadersCount   int32
	headersFetchProgress  int32
	headersFetchTimeTaken int64

	addressDiscoveryProgress int32
	totalDiscoveryTime       int64

	rescanProgress      int32
	currentRescanHeight int32
}

// InitSyncInfo returns a new SyncInfo pointer with default values set
func InitSyncInfo() *SyncInfo {
	return &SyncInfo{
		headersFetchTimeTaken: -1,
		totalDiscoveryTime:    -1,
	}
}

// readableSyncInfo holds information about an ongoing sync op for display on the different UIs.
// Not to be used directly but via `SyncInfo.Read()`
type readableSyncInfo struct {
	Status         Status
	ConnectedPeers int32
	Error          string
	Done           bool

	CurrentStep        SyncStep
	TotalSyncProgress  int32
	TotalTimeRemaining string

	TotalHeadersToFetch   int32
	DaysBehind            string
	FetchedHeadersCount   int32
	HeadersFetchProgress  int32
	HeadersFetchTimeTaken int64

	AddressDiscoveryProgress int32
	TotalDiscoveryTime       int64

	RescanProgress      int32
	CurrentRescanHeight int32
}

// Read returns the current sync op info from private variables after locking the mutex for reading
func (syncInfo *SyncInfo) Read() *readableSyncInfo {
	syncInfo.RLock()
	defer syncInfo.RUnlock()

	return &readableSyncInfo{
		syncInfo.status,
		syncInfo.connectedPeers,
		syncInfo.error,
		syncInfo.done,
		syncInfo.currentStep,
		syncInfo.totalSyncProgress,
		syncInfo.totalTimeRemaining,
		syncInfo.totalHeadersToFetch,
		syncInfo.daysBehind,
		syncInfo.fetchedHeadersCount,
		syncInfo.headersFetchProgress,
		syncInfo.headersFetchTimeTaken,
		syncInfo.addressDiscoveryProgress,
		syncInfo.totalDiscoveryTime,
		syncInfo.rescanProgress,
		syncInfo.currentRescanHeight,
	}
}

// Write saves info for ongoing sync op to private variables after locking mutex for writing
func (syncInfo *SyncInfo) Write(info *readableSyncInfo, status Status) {
	syncInfo.Lock()
	defer syncInfo.Unlock()

	syncInfo.status = status
	syncInfo.connectedPeers = info.ConnectedPeers
	syncInfo.error = info.Error
	syncInfo.done = info.Done

	syncInfo.currentStep = info.CurrentStep
	syncInfo.totalSyncProgress = info.TotalSyncProgress
	syncInfo.totalTimeRemaining = info.TotalTimeRemaining

	syncInfo.totalHeadersToFetch = info.TotalHeadersToFetch
	syncInfo.daysBehind = info.DaysBehind
	syncInfo.fetchedHeadersCount = info.FetchedHeadersCount
	syncInfo.headersFetchProgress = info.HeadersFetchProgress
	syncInfo.headersFetchTimeTaken = info.HeadersFetchTimeTaken

	syncInfo.addressDiscoveryProgress = info.AddressDiscoveryProgress
	syncInfo.totalDiscoveryTime = info.TotalDiscoveryTime

	syncInfo.rescanProgress = info.RescanProgress
	syncInfo.currentRescanHeight = info.CurrentRescanHeight
}
