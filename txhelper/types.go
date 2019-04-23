package txhelper

import (
	"github.com/decred/dcrwallet/wallet"
)

var (
	transactionDirectionNames = []string{"Sent", "Received", "Transferred", "Unclear"}
)

const (
	// TransactionDirectionSent for transactions sent to external address(es) from wallet
	TransactionDirectionSent TransactionDirection = iota

	// TransactionDirectionReceived for transactions received from external address(es) into wallet
	TransactionDirectionReceived

	// TransactionDirectionTransferred for transactions sent from wallet to internal address(es)
	TransactionDirectionTransferred

	// TransactionDirectionUnclear for unrecognized transaction directions
	TransactionDirectionUnclear
)

type TransactionDirection int8

func (direction TransactionDirection) String() string {
	if direction <= TransactionDirectionUnclear {
		return transactionDirectionNames[direction]
	} else {
		return transactionDirectionNames[TransactionDirectionUnclear]
	}
}

type TransactionDestination struct {
	Address string
	Amount  float64
	SendMax bool
}

type Transaction struct {
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

	Direction TransactionDirection `storm:"index" json:"direction"`
	Amount    int64                `json:"amount"`
	Inputs    []*TxInput           `json:"inputs"`
	Outputs   []*TxOutput          `json:"outputs"`

	// Vote Info
	VoteVersion    int32  `json:"vote_version"`
	LastBlockValid bool   `json:"last_block_valid"`
	VoteBits       string `json:"vote_bits"`
}

type TxInput struct {
	PreviousTransactionHash  string `json:"previous_transaction_hash"`
	PreviousTransactionIndex int32  `json:"previous_transaction_index"`
	PreviousOutpoint         string `json:"previous_outpoint"`
	AmountIn                 int64  `json:"amount_in"`
	*WalletInput
}

type TxOutput struct {
	Index      int32  `json:"index"`
	Amount     int64  `json:"amount"`
	Version    int32  `json:"version"`
	ScriptType string `json:"script_type"`
	Address    string `json:"address"`
}

// TxInfoFromWallet contains tx data that relates to the querying wallet.
// This info is used with `DecodeTransaction` to compose the entire details of a transaction.
type TxInfoFromWallet struct {
	Hex               string
	Timestamp         int64
	BlockHeight       int32
	Inputs            []*WalletInput
	TotalInputAmount  int64
	TotalOutputAmount int64
}

type WalletInput struct {
	Index           int32  `json:"index"`
	PreviousAccount int32  `json:"previous_account"`
	AccountName     string `json:"account_name"`
}

func FormatTransactionType(txType wallet.TransactionType) string {
	switch txType {
	case wallet.TransactionTypeCoinbase:
		return "Coinbase"
	case wallet.TransactionTypeTicketPurchase:
		return "Ticket"
	case wallet.TransactionTypeVote:
		return "Vote"
	case wallet.TransactionTypeRevocation:
		return "Revocation"
	default:
		return "Regular"
	}
}
