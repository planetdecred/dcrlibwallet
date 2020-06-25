package dcrlibwallet

import "github.com/decred/dcrwallet/wallet/v3"

type WalletsIterator struct {
	currentIndex int
	wallets      []*Wallet
}

type BlockInfo struct {
	Height    int32
	Timestamp int64
}

type Amount struct {
	AtomValue int64
	DcrValue  float64
}

type TxFeeAndSize struct {
	Fee                 *Amount
	EstimatedSignedSize int
}

type UnsignedTransaction struct {
	UnsignedTransaction       []byte
	EstimatedSignedSize       int
	ChangeIndex               int
	TotalOutputAmount         int64
	TotalPreviousOutputAmount int64
}

type Balance struct {
	Total                   int64
	Spendable               int64
	ImmatureReward          int64
	ImmatureStakeGeneration int64
	LockedByTickets         int64
	VotingAuthority         int64
	UnConfirmed             int64
}

type Account struct {
	WalletID         int
	Number           int32
	Name             string
	Balance          *Balance
	TotalBalance     int64
	ExternalKeyCount int32
	InternalKeyCount int32
	ImportedKeyCount int32
}

type AccountsIterator struct {
	currentIndex int
	accounts     []*Account
}

type Accounts struct {
	Count              int
	Acc                []*Account
	CurrentBlockHash   []byte
	CurrentBlockHeight int32
}

/** begin sync-related types */

type SyncProgressListener interface {
	OnSyncStarted(wasRestarted bool)
	OnPeerConnectedOrDisconnected(numberOfConnectedPeers int32)
	OnHeadersFetchProgress(headersFetchProgress *HeadersFetchProgressReport)
	OnAddressDiscoveryProgress(addressDiscoveryProgress *AddressDiscoveryProgressReport)
	OnHeadersRescanProgress(headersRescanProgress *HeadersRescanProgressReport)
	OnSyncCompleted()
	OnSyncCanceled(willRestart bool)
	OnSyncEndedWithError(err error)
	Debug(debugInfo *DebugInfo)
}

type GeneralSyncProgress struct {
	TotalSyncProgress         int32 `json:"totalSyncProgress"`
	TotalTimeRemainingSeconds int64 `json:"totalTimeRemainingSeconds"`
}

type HeadersFetchProgressReport struct {
	*GeneralSyncProgress
	TotalHeadersToFetch    int32 `json:"totalHeadersToFetch"`
	CurrentHeaderHeight    int32 `json:"currentHeaderHeight"`
	CurrentHeaderTimestamp int64 `json:"currentHeaderTimestamp"`
	HeadersFetchProgress   int32 `json:"headersFetchProgress"`
}

type AddressDiscoveryProgressReport struct {
	*GeneralSyncProgress
	AddressDiscoveryProgress int32 `json:"addressDiscoveryProgress"`
	WalletID                 int   `json:"walletID"`
}

type HeadersRescanProgressReport struct {
	*GeneralSyncProgress
	TotalHeadersToScan  int32 `json:"totalHeadersToScan"`
	CurrentRescanHeight int32 `json:"currentRescanHeight"`
	RescanProgress      int32 `json:"rescanProgress"`
	RescanTimeRemaining int64 `json:"rescanTimeRemaining"`
	WalletID            int   `json:"walletID"`
}

type DebugInfo struct {
	TotalTimeElapsed          int64
	TotalTimeRemaining        int64
	CurrentStageTimeElapsed   int64
	CurrentStageTimeRemaining int64
}

/** end sync-related types */

/** begin tx-related types */

type TxAndBlockNotificationListener interface {
	OnTransaction(transaction string)
	OnBlockAttached(walletID int, blockHeight int32)
	OnTransactionConfirmed(walletID int, hash string, blockHeight int32)
}

type BlocksRescanProgressListener interface {
	OnBlocksRescanStarted(walletID int)
	OnBlocksRescanProgress(*HeadersRescanProgressReport)
	OnBlocksRescanEnded(walletID int, err error)
}

// Transaction is used with storm for tx indexing operations.
// For faster queries, the `Hash`, `Type` and `Direction` fields are indexed.
type Transaction struct {
	WalletID    int    `json:"walletID"`
	Hash        string `storm:"id,unique" json:"hash"`
	Type        string `storm:"index" json:"type"`
	Hex         string `json:"hex"`
	Timestamp   int64  `json:"timestamp"`
	BlockHeight int32  `json:"block_height"`

	Version  int32 `json:"version"`
	LockTime int32 `json:"lock_time"`
	Expiry   int32 `json:"expiry"`
	Fee      int64 `json:"fee"`
	FeeRate  int64 `json:"fee_rate"`
	Size     int   `json:"size"`

	Direction int32       `storm:"index" json:"direction"`
	Amount    int64       `json:"amount"`
	Inputs    []*TxInput  `json:"inputs"`
	Outputs   []*TxOutput `json:"outputs"`

	// Vote Info
	VoteVersion        int32  `json:"vote_version"`
	LastBlockValid     bool   `json:"last_block_valid"`
	VoteBits           string `json:"vote_bits"`
	VoteReward         int64  `json:"vote_reward"`
	TicketSpentHash    string `storm:"unique" json:"ticket_spent_hash"`
	DaysToVoteOrRevoke int32  `json:"days_to_vote_revoke"`
}

type TxInput struct {
	PreviousTransactionHash  string `json:"previous_transaction_hash"`
	PreviousTransactionIndex int32  `json:"previous_transaction_index"`
	PreviousOutpoint         string `json:"previous_outpoint"`
	Amount                   int64  `json:"amount"`
	AccountName              string `json:"account_name"`
	AccountNumber            int32  `json:"account_number"`
}

type TxOutput struct {
	Index         int32  `json:"index"`
	Amount        int64  `json:"amount"`
	Version       int32  `json:"version"`
	ScriptType    string `json:"script_type"`
	Address       string `json:"address"`
	Internal      bool   `json:"internal"`
	AccountName   string `json:"account_name"`
	AccountNumber int32  `json:"account_number"`
}

// TxInfoFromWallet contains tx data that relates to the querying wallet.
// This info is used with `DecodeTransaction` to compose the entire details of a transaction.
type TxInfoFromWallet struct {
	WalletID    int
	Hex         string
	Timestamp   int64
	BlockHeight int32
	Inputs      []*WalletInput
	Outputs     []*WalletOutput
}

type WalletInput struct {
	Index    int32 `json:"index"`
	AmountIn int64 `json:"amount_in"`
	*WalletAccount
}

type WalletOutput struct {
	Index     int32  `json:"index"`
	AmountOut int64  `json:"amount_out"`
	Internal  bool   `json:"internal"`
	Address   string `json:"address"`
	*WalletAccount
}

type WalletAccount struct {
	AccountNumber int32  `json:"account_number"`
	AccountName   string `json:"account_name"`
}

type TransactionDestination struct {
	Address    string
	AtomAmount int64
	SendMax    bool
}

/** end tx-related types */

/** begin ticket-related types */

type PurchaseTicketsRequest struct {
	Account               uint32
	RequiredConfirmations uint32
	NumTickets            uint32
	Passphrase            []byte
	Expiry                uint32
	TxFee                 int64
	TicketAddress         string
	PoolAddress           string
	PoolFees              float64
	TicketFee             int64
}

type GetTicketsRequest struct {
	StartingBlockHash   []byte
	StartingBlockHeight int32
	EndingBlockHash     []byte
	EndingBlockHeight   int32
	TargetTicketCount   int32
}

type TicketInfo struct {
	BlockHeight int32
	Status      string
	Ticket      *wallet.TransactionSummary
	Spender     *wallet.TransactionSummary
}

type TicketPriceResponse struct {
	TicketPrice int64
	Height      int32
}

type VSPTicketPurchaseInfo struct {
	PoolAddress   string
	PoolFees      float64
	Script        string
	TicketAddress string
}

/** end ticket-related types */
