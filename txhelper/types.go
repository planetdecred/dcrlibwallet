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
	Hash        string `storm:"id,unique"`
	Hex         string
	Timestamp   int64
	BlockHeight int32
	Type        string

	Version  int32
	LockTime int32
	Expiry   int32
	Fee      int64
	FeeRate  int64
	Size     int

	Direction TransactionDirection
	Amount    int64
	Inputs    []*TxInput
	Outputs   []*TxOutput

	// Vote Info
	VoteVersion    int32
	LastBlockValid bool
	VoteBits       string
}

type TxInput struct {
	PreviousTransactionHash  string
	PreviousTransactionIndex int32
	PreviousOutpoint         string
	AmountIn                 int64
	*WalletInput
}

type WalletInput struct {
	Index           int32
	PreviousAccount int32
	AccountName     string
}

type TxOutput struct {
	Index      int32
	Amount     int64
	Version    int32
	ScriptType string
	Address    string
}

type WalletTx struct {
	RawTx             string
	Timestamp         int64
	BlockHeight       int32
	Confirmations     int32
	Inputs            []*WalletInput
	TotalInputAmount  int64
	TotalOutputAmount int64
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
