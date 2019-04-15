package blockchainsync

type ErrorCode uint8

const (
	// Sync States
	START    = "start"
	FINISH   = "finish"
	PROGRESS = "progress"

	// Sync Error Codes
	UnexpectedError  ErrorCode = iota
	ContextCanceled
	DeadlineExceeded
	InvalidPeerAddress
)


type ProgressListener interface {
	OnPeerConnected(peerCount int32)
	OnPeerDisconnected(peerCount int32)
	OnFetchMissingCFilters(missingCFiltersStart, missingCFiltersEnd int32, state string)
	OnFetchedHeaders(fetchedHeadersCount int32, lastHeaderTime int64, state string)
	OnDiscoveredAddresses(state string)
	OnRescan(rescannedThrough int32, state string)
	OnIndexTransactions(totalIndex int32)
	OnSynced(synced bool)
	OnSyncError(code ErrorCode, err error)
}
