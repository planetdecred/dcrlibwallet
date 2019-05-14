package syncprogressestimator

const (
	SyncStateStart    = "start"
	SyncStateProgress = "progress"
	SyncStateFinish   = "finish"
)

type EstimatedSyncProgressListener interface {
	OnGeneralSyncProgress(report GeneralSyncProgressReport)
	OnHeadersFetchProgress(report HeadersFetchProgressReport, generalProgress GeneralSyncProgressReport)
	OnAddressDiscoveryProgress(report AddressDiscoveryProgressReport, generalProgress GeneralSyncProgressReport)
	OnHeadersRescanProgress(report HeadersRescanProgressReport, generalProgress GeneralSyncProgressReport)
}

type GeneralSyncProgressReport struct {
	Status         string `json:"status"`
	ConnectedPeers int32  `json:"connectedPeers"`
	Error          string `json:"error"`
	Done           bool   `json:"done"`

	TotalSyncProgress         int32 `json:"totalSyncProgress"`
	TotalTimeRemainingSeconds int64 `json:"totalTimeRemainingSeconds"`
}

type HeadersFetchProgressReport struct {
	TotalHeadersToFetch    int32 `json:"totalHeadersToFetch"`
	CurrentHeaderTimestamp int64 `json:"currentHeaderTimestamp"`
	FetchedHeadersCount    int32 `json:"fetchedHeadersCount"`
	HeadersFetchProgress   int32 `json:"headersFetchProgress"`
}

type AddressDiscoveryProgressReport struct {
	AddressDiscoveryProgress int32 `json:"addressDiscoveryProgress"`
}

type HeadersRescanProgressReport struct {
	TotalHeadersToScan  int32 `json:"totalHeadersToScan"`
	RescanProgress      int32 `json:"rescanProgress"`
	CurrentRescanHeight int32 `json:"currentRescanHeight"`
}
