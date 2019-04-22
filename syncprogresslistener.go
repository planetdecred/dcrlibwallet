package dcrlibwallet

type SyncErrorCode uint16

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
	OnIndexTransactions(totalIndex int32)
	OnSynced(synced bool)
	OnSyncError(code uint16, err error) // use int32 to allow gomobile bind
}
