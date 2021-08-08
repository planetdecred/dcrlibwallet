package walletdata

import (
	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/planetdecred/dcrlibwallet/txhelper"
)

const (
	TxFilterAll         int32 = 0
	TxFilterSent        int32 = 1
	TxFilterReceived    int32 = 2
	TxFilterTransferred int32 = 3
	TxFilterStaking     int32 = 4
	TxFilterCoinBase    int32 = 5
	TxFilterRegular     int32 = 6
	TxFilterMixed       int32 = 7
	TxFilterVoted       int32 = 8
	TxFilterRevoked     int32 = 9
	TxFilterImmature    int32 = 10
	TxFilterLive        int32 = 11
	TxFilterExpired     int32 = 12
	TxFilterUnmined     int32 = 13
)

func (db *DB) prepareTxQuery(txFilter, requiredConfirmations, bestBlock int32) (query storm.Query) {
	switch txFilter {
	case TxFilterSent:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeRegular),
			q.Eq("Direction", txhelper.TxDirectionSent),
		)
	case TxFilterReceived:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeRegular),
			q.Eq("Direction", txhelper.TxDirectionReceived),
		)
	case TxFilterTransferred:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeRegular),
			q.Eq("Direction", txhelper.TxDirectionTransferred),
		)
	case TxFilterStaking:
		query = db.walletDataDB.Select(
			q.Or(
				q.Eq("Type", txhelper.TxTypeTicketPurchase),
				q.Eq("Type", txhelper.TxTypeVote),
				q.Eq("Type", txhelper.TxTypeRevocation),
			),
		)
	case TxFilterCoinBase:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeCoinBase),
		)
	case TxFilterRegular:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeRegular),
		)
	case TxFilterMixed:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeMixed),
		)
	case TxFilterVoted:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeVote),
		)
	case TxFilterRevoked:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeRevocation),
		)
	case TxFilterImmature:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeTicketPurchase),
			q.And(
				q.Lte("BlockHeight", bestBlock-requiredConfirmations), // must be confirmed
				q.Gt("BlockHeight", bestBlock-int32(db.chainParams.TicketMaturity)),
			),
		)
	case TxFilterLive:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeTicketPurchase),
			q.Eq("TicketSpender", ""),                                            // not spent by a vote or revoke
			q.Gt("BlockHeight", 0),                                               // mined
			q.Lte("BlockHeight", bestBlock-int32(db.chainParams.TicketMaturity)), // must be matured
			q.Or( // must not be expired (tx with expiry=0 are excluded)
				q.Gte("Expiry", bestBlock),
				q.Eq("Expiry", 0),
			),
		)
	case TxFilterExpired:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeTicketPurchase),
			q.Eq("TicketSpender", ""),
			q.And(
				q.Lte("Expiry", bestBlock),
				q.Not(q.Eq("Expiry", 0)),
			),
		)
	case TxFilterUnmined:
		query = db.walletDataDB.Select(
			q.Eq("Type", txhelper.TxTypeTicketPurchase),
			q.Or(
				q.Eq("BlockHeight", -1),
				q.Gt("BlockHeight", bestBlock-requiredConfirmations),
			),
		)
	default:
		query = db.walletDataDB.Select(
			q.True(),
		)
	}

	return
}
