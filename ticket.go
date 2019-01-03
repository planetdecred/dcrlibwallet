package dcrlibwallet

import (
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet"
)

type TicketStatus int32

const (
	TicketStatusUnknown TicketStatus = iota
	TicketStatusUnmined
	TicketStatusImmature
	TicketStatusLive
	TicketStatusVoted
	TicketStatusMissed
	TicketStatusExpired
	TicketStatusRevoked
)

var TicketStatusNames = map[TicketStatus]string{
	TicketStatusUnknown: "UNKNOWN",
	TicketStatusUnmined: "UNMINED",
	TicketStatusImmature: "IMMATURE",
	TicketStatusLive: "LIVE",
	TicketStatusVoted: "VOTED",
	TicketStatusMissed: "MISSED",
	TicketStatusExpired: "EXPIRED",
	TicketStatusRevoked: "REVOKED",
}
var TicketStatusValues = map[string]TicketStatus{
	"UNKNOWN":  TicketStatusUnknown,
	"UNMINED":  TicketStatusUnmined,
	"IMMATURE": TicketStatusImmature,
	"LIVE":     TicketStatusLive,
	"VOTED":    TicketStatusVoted,
	"MISSED":   TicketStatusMissed,
	"EXPIRED":  TicketStatusExpired,
	"REVOKED":  TicketStatusRevoked,
}

func (status TicketStatus) String() string {
	name, ok := TicketStatusNames[status]
	if !ok {
		return TicketStatusNames[TicketStatusUnknown]
	}
	return name
}

type PurchaseTicketsRequest struct {
	Passphrase            []byte
	Account               uint32
	SpendLimit            int64
	RequiredConfirmations uint32
	TicketAddress         string
	NumTickets            uint32
	PoolAddress           string
	PoolFees              float64
	Expiry                uint32
	TxFee                 int64
	TicketFee             int64
}

type GetTicketsRequest struct {
	StartingBlockHash   []byte
	StartingBlockHeight int32
	EndingBlockHash     []byte
	EndingBlockHeight   int32
	TargetTicketCount   int32
}

type GetTicketsResponse struct {
	Ticket       *wallet.TicketSummary
	Block        *BlockDetails
	TicketStatus TicketStatus
}

func marshalGetTicketBlockDetails(v *wire.BlockHeader) *BlockDetails {
	if v == nil || v.Height < 0 {
		return nil
	}

	blockHash := v.BlockHash()
	return &BlockDetails{
		Hash:      blockHash[:],
		Height:    int32(v.Height),
		Timestamp: v.Timestamp.Unix(),
	}
}

func marshalTicketDetails(ticket *wallet.TicketSummary) TicketStatus {
	var ticketStatus = TicketStatusLive
	switch ticket.Status {
	case wallet.TicketStatusExpired:
		ticketStatus = TicketStatusExpired
	case wallet.TicketStatusImmature:
		ticketStatus = TicketStatusImmature
	case wallet.TicketStatusVoted:
		ticketStatus = TicketStatusVoted
	case wallet.TicketStatusRevoked:
		ticketStatus = TicketStatusRevoked
	case wallet.TicketStatusUnmined:
		ticketStatus = TicketStatusUnmined
	case wallet.TicketStatusMissed:
		ticketStatus = TicketStatusMissed
	case wallet.TicketStatusUnknown:
		ticketStatus = TicketStatusUnknown
	}

	return ticketStatus
}

type BlockDetails struct {
	Hash      []byte
	Height    int32
	Timestamp int64
}
