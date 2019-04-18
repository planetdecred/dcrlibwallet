package defaultsynclistener

import "sync"

// progressReport is used by `defaultSyncListener` to store relevant progress information about an ongoing sync operation.
// Not to be used directly but via `ProgressReport.Read()`
type progressReport struct {
	Status         SyncStatus
	ConnectedPeers int32  `json:"connectedPeers"`
	Error          string `json:"error"`
	Done           bool   `json:"done"`

	CurrentStep        SyncStep `json:"currentStep"`
	TotalSyncProgress  int32    `json:"totalSyncProgress"`
	TotalTimeRemaining string   `json:"totalTimeRemaining"`

	TotalHeadersToFetch  int32  `json:"totalHeadersToFetch"`
	DaysBehind           string `json:"daysBehind"`
	FetchedHeadersCount  int32  `json:"fetchedHeadersCount"`
	HeadersFetchProgress int32  `json:"headersFetchProgress"`

	AddressDiscoveryProgress int32 `json:"addressDiscoveryProgress"`

	RescanProgress      int32 `json:"rescanProgress"`
	CurrentRescanHeight int32 `json:"currentRescanHeight"`
}

// ProgressReport holds information about a sync op in a private variable
// to prevent reading/writing the values directly during a sync op.
type ProgressReport struct {
	sync.RWMutex
	progressReport
}

// InitProgressReport returns a new ProgressReport pointer with default values set.
func InitProgressReport() *ProgressReport {
	return &ProgressReport{}
}

// Read returns the current sync op progress report from the private `progressReport` variable
// after locking the mutex for reading.
func (report *ProgressReport) Read() *progressReport {
	report.RLock()
	defer report.RUnlock()
	return &report.progressReport
}

// Update reads the current progress report data and passes it to the provided `updateProgressReport` function for updating.
// After the report is updated, mutex is locked for writing and the updated report is saved to the private `progressReport` variable.
func (report *ProgressReport) Update(status SyncStatus, updateProgressReport func(*progressReport)) {
	latestReport := report.Read()
	updateProgressReport(latestReport)

	report.Lock()

	report.progressReport = progressReport{
		latestReport.Status,
		latestReport.ConnectedPeers,
		latestReport.Error,
		latestReport.Done,

		latestReport.CurrentStep,
		latestReport.TotalSyncProgress,
		latestReport.TotalTimeRemaining,
		latestReport.TotalHeadersToFetch,
		latestReport.DaysBehind,
		latestReport.FetchedHeadersCount,
		latestReport.HeadersFetchProgress,

		latestReport.AddressDiscoveryProgress,

		latestReport.RescanProgress,
		latestReport.CurrentRescanHeight,
	}

	report.Unlock()
}
