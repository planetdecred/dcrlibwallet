package defaultsynclistener

type SyncStatus uint8

const (
	SyncStatusNotStarted SyncStatus = iota
	SyncStatusSuccess
	SyncStatusError
	SyncStatusInProgress
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
