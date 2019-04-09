package dcrlibwallet

import "github.com/decred/dcrwallet/wallet"

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
	TicketStatusUnknown:  "UNKNOWN",
	TicketStatusUnmined:  "UNMINED",
	TicketStatusImmature: "IMMATURE",
	TicketStatusLive:     "LIVE",
	TicketStatusVoted:    "VOTED",
	TicketStatusMissed:   "MISSED",
	TicketStatusExpired:  "EXPIRED",
	TicketStatusRevoked:  "REVOKED",
}

func (status TicketStatus) String() string {
	name, ok := TicketStatusNames[status]
	if !ok {
		return TicketStatusNames[TicketStatusUnknown]
	}
	return name
}

func ticketStatus(ticket *wallet.TicketSummary) TicketStatus {
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
