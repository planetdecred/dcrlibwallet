package blockchainsync

const (
	// Sync States
	START    = "start"
	FINISH   = "finish"
	PROGRESS = "progress"
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
	/*
	* Handled Error Codes
	* -1 - Unexpected Error
	*  1 - Context Canceled
	*  2 - Deadline Exceeded
	*  3 - Invalid Address
	 */
	OnSyncError(code int, err error)
}
