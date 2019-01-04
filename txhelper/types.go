package txhelper

import "github.com/decred/dcrd/dcrutil"

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
}

type DecodedTransaction struct {
	Hash     string
	Type     string
	Version  int32
	LockTime int32
	Expiry   int32
	Inputs   []DecodedInput
	Outputs  []DecodedOutput

	//Vote Info
	VoteVersion    int32
	LastBlockValid bool
	VoteBits       string
}

type DecodedInput struct {
	PreviousTransactionHash  string
	PreviousTransactionIndex int32
	PreviousOutpoint         string
	AmountIn                 dcrutil.Amount
}

type DecodedOutput struct {
	Index      int32
	Value      dcrutil.Amount
	Internal   bool
	Version    int32
	ScriptType string
	Addresses  []string
}
