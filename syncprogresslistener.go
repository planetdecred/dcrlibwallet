package dcrlibwallet

type SyncErrorCode uint8

const (
	ErrorCodeUnexpectedError SyncErrorCode = iota
	ErrorCodeContextCanceled
	ErrorCodeDeadlineExceeded
	ErrorCodeInvalidPeerAddress
)

type SyncProgressListener interface {
	OnPeerConnected(peerCount int32)
	OnPeerDisconnected(peerCount int32)
	OnFetchMissingCFilters(missingCFiltersStart, missingCFiltersEnd int32, state string)
	OnFetchedHeaders(fetchedHeadersCount int32, lastHeaderTime int64, state string)
	OnDiscoveredAddresses(state string)
	OnRescan(rescannedThrough int32, state string)
	OnIndexTransactions(totalIndexed int32)
	OnSynced(synced bool)
	OnSyncError(code SyncErrorCode, err error)
}
