package defaultsynclistener

type SyncStatus uint8

const (
	SyncStatusInProgress SyncStatus = iota
	SyncStatusSuccess
	SyncStatusError
)

type SyncOp uint8

const (
	PeersCountUpdate SyncOp = iota
	CurrentStepUpdate
	SyncDone
)

type SyncStep uint8

const (
	FetchingBlockHeaders SyncStep = iota
	DiscoveringUsedAddresses
	ScanningBlockHeaders
)
