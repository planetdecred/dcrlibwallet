package blockchainsync

type Status uint8

const (
	StatusNotStarted Status = iota
	StatusSuccess
	StatusError
	StatusInProgress
)

type FetchHeadersData struct {
	StartHeaderHeight   int32
	CurrentHeaderHeight int32
	BeginFetchTimeStamp int64
}

type FetchHeadersProgressReport struct {
	FetchedHeadersCount       int32
	LastHeaderTime            int64
	EstimatedFinalBlockHeight int32
}
