package txhelper

import (
	"github.com/decred/dcrwallet/rpc/walletrpc"
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

type DecodedTransaction struct {
	Hash     string
	Type     string
	Version  int32
	LockTime int32
	Expiry   int32
	Fee      int64
	FeeRate  int64
	Size     int
	Inputs   []*DecodedInput
	Outputs  []*DecodedOutput

	//Vote Info
	VoteVersion    int32
	LastBlockValid bool
	VoteBits       string
}

type DecodedInput struct {
	PreviousTransactionHash  string
	PreviousTransactionIndex int32
	PreviousOutpoint         string
	AmountIn                 int64
}

type DecodedOutput struct {
	Index      int32
	Value      int64
	Internal   bool
	Version    int32
	ScriptType string
	Addresses  []*AddressInfo
}

// AddressInfo holds information about an address
// If the address belongs to the querying wallet, IsMine will be true and the AccountNumber and AccountName values will be populated
type AddressInfo struct {
	Address       string
	IsMine        bool
	AccountNumber uint32
	AccountName   string
}

func TransactionType(txType wallet.TransactionType) string {
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

func RPCTransactionType(txType walletrpc.TransactionDetails_TransactionType) string {
	switch txType {
	case walletrpc.TransactionDetails_COINBASE:
		return "Coinbase"
	case walletrpc.TransactionDetails_TICKET_PURCHASE:
		return "Ticket"
	case walletrpc.TransactionDetails_VOTE:
		return "Vote"
	case walletrpc.TransactionDetails_REVOCATION:
		return "Revocation"
	default:
		return "Regular"
	}
}
