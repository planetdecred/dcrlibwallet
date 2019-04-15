package blockchainsync

import "sync"

// PrivateSyncInfo holds information about a sync op in private variables
// to prevent reading/writing the values directly during a sync op.
type PrivateSyncInfo struct {
	sync.RWMutex

	status         Status
	connectedPeers int32
	error          string
	done           bool

	currentStep        int
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

// NewPrivateInfo returns a new PrivateSyncInfo pointer with default values set
func NewPrivateInfo() *PrivateSyncInfo {
	return &PrivateSyncInfo{
		headersFetchTimeTaken: -1,
		totalDiscoveryTime:    -1,
	}
}

// info holds information about an ongoing sync op for display on the different UIs.
// Not to be used directly but via `PrivateSyncInfo.Read()`
type info struct {
	Status         Status
	ConnectedPeers int32
	Error          string
	Done           bool

	CurrentStep        int
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
func (privateInfo *PrivateSyncInfo) Read() *info {
	privateInfo.RLock()
	defer privateInfo.RUnlock()

	return &info{
		privateInfo.status,
		privateInfo.connectedPeers,
		privateInfo.error,
		privateInfo.done,
		privateInfo.currentStep,
		privateInfo.totalSyncProgress,
		privateInfo.totalTimeRemaining,
		privateInfo.totalHeadersToFetch,
		privateInfo.daysBehind,
		privateInfo.fetchedHeadersCount,
		privateInfo.headersFetchProgress,
		privateInfo.headersFetchTimeTaken,
		privateInfo.addressDiscoveryProgress,
		privateInfo.totalDiscoveryTime,
		privateInfo.rescanProgress,
		privateInfo.currentRescanHeight,
	}
}

// Write saves info for ongoing sync op to private variables after locking mutex for writing
func (privateInfo *PrivateSyncInfo) Write(info *info, status Status) {
	privateInfo.Lock()
	defer privateInfo.Unlock()

	privateInfo.status = status
	privateInfo.connectedPeers = info.ConnectedPeers
	privateInfo.error = info.Error
	privateInfo.done = info.Done

	privateInfo.currentStep = info.CurrentStep
	privateInfo.totalSyncProgress = info.TotalSyncProgress
	privateInfo.totalTimeRemaining = info.TotalTimeRemaining

	privateInfo.totalHeadersToFetch = info.TotalHeadersToFetch
	privateInfo.daysBehind = info.DaysBehind
	privateInfo.fetchedHeadersCount = info.FetchedHeadersCount
	privateInfo.headersFetchProgress = info.HeadersFetchProgress
	privateInfo.headersFetchTimeTaken = info.HeadersFetchTimeTaken

	privateInfo.addressDiscoveryProgress = info.AddressDiscoveryProgress
	privateInfo.totalDiscoveryTime = info.TotalDiscoveryTime

	privateInfo.rescanProgress = info.RescanProgress
	privateInfo.currentRescanHeight = info.CurrentRescanHeight
}
