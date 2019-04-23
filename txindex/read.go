package txindex

import (
	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
)

const MaxReOrgBlocks = 6

func (db *DB) CountTx(filter *ReadFilter) (int, error) {
	return db.createTxQuery(filter).Count(&txhelper.Transaction{})
}

func (db *DB) Read(offset, limit int32, filter *ReadFilter) (transactions []*txhelper.Transaction, err error) {
	query := db.createTxQuery(filter).OrderBy("Timestamp").Reverse()

	if offset > 0 {
		query = query.Skip(int(offset))
	}
	if limit > 0 {
		query = query.Limit(int(limit))
	}

	err = query.Find(&transactions)
	if err == storm.ErrNotFound {
		err = nil
	}

	return
}

func (db *DB) createTxQuery(filter *ReadFilter) (query storm.Query) {
	if filter == nil {
		query = db.txDB.Select()
	} else {
		var filters []q.Matcher
		for _, txType := range filter.typeFilter {
			filters = append(filters, q.StrictEq("Type", txType))
		}
		for _, direction := range filter.directionFilter {
			filters = append(filters, q.StrictEq("Direction", direction))
		}
		query = db.txDB.Select(filters...)
	}
	return
}

// ReadIndexingStartBlock checks if a end block height was saved from last indexing operation.
// If so, the end block height - MaxReOrgBlocks is returned. Otherwise, 0 (default int32 value) is returned.
func (db *DB) ReadIndexingStartBlock() (startBlockHeight int32, err error) {
	err = db.txDB.Get(TxBucketName, KeyEndBlock, &startBlockHeight)
	if err != nil && err != storm.ErrNotFound {
		return
	}

	err = nil
	startBlockHeight -= MaxReOrgBlocks
	if startBlockHeight < 0 {
		startBlockHeight = 0
	}
	return
}
