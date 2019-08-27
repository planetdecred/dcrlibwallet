package txindex

import (
	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
)

const (
	TxFilterAll         int32 = 0
	TxFilterSent        int32 = 1
	TxFilterReceived    int32 = 2
	TxFilterTransferred int32 = 3
	TxFilterStaking     int32 = 4
	TxFilterCoinBase    int32 = 5

	TxDirectionInvalid     int32 = -1
	TxDirectionSent        int32 = 0
	TxDirectionReceived    int32 = 1
	TxDirectionTransferred int32 = 2

	TxTypeRegular        = "REGULAR"
	TxTypeCoinBase       = "COINBASE"
	TxTypeTicketPurchase = "TICKET_PURCHASE"
	TxTypeVote           = "VOTE"
	TxTypeRevocation     = "REVOCATION"
)

func DetermineTxFilter(txType string, txDirection int32) int32 {
	if txType == TxTypeCoinBase {
		return TxFilterCoinBase
	}
	if txType != TxTypeRegular {
		return TxFilterStaking
	}

	switch txDirection {
	case TxDirectionSent:
		return TxFilterSent
	case TxDirectionReceived:
		return TxFilterReceived
	default:
		return TxFilterTransferred
	}
}

func (db *DB) prepareTxQuery(txFilter int32) (query storm.Query) {
	switch txFilter {
	case TxFilterSent:
		query = db.txDB.Select(
			q.Eq("Direction", TxDirectionSent),
		)
	case TxFilterReceived:
		query = db.txDB.Select(
			q.Eq("Direction", TxDirectionReceived),
		)
	case TxFilterTransferred:
		query = db.txDB.Select(
			q.Eq("Direction", TxDirectionTransferred),
		)
	case TxFilterStaking:
		query = db.txDB.Select(
			q.Not(
				q.Eq("Type", TxTypeRegular),
				q.Eq("Type", TxTypeCoinBase),
			),
		)
	case TxFilterCoinBase:
		query = db.txDB.Select(
			q.Eq("Type", TxTypeCoinBase),
		)
	default:
		query = db.txDB.Select(
			q.True(),
		)
	}

	query = query.OrderBy("Timestamp").Reverse()
	return
}
