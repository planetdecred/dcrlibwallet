package txindex

import (
	"fmt"

	"github.com/asdine/storm"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
)

const KeyEndBlock = "EndBlock"

func (db *DB) SaveOrUpdate(tx *txhelper.Transaction) error {
	var oldTx txhelper.Transaction
	err := db.TxDB.One("Hash", tx.Hash, &oldTx)

	if err != nil && err != storm.ErrNotFound {
		return fmt.Errorf("error checking if tx was already indexed: %s", err.Error())
	}

	if err != storm.ErrNotFound {
		// delete old tx before saving new
		err = db.TxDB.DeleteStruct(&oldTx)
		if err != nil {
			return fmt.Errorf("error deleting previously indexed tx: %s", err.Error())
		}
	}

	return db.TxDB.Save(tx)
}

func (db *DB) SaveLastIndexPoint(endBlockHeight int32) error {
	err := db.TxDB.Set(TxBucketName, KeyEndBlock, &endBlockHeight)
	if err != nil {
		return fmt.Errorf("error setting block height for last indexed tx: %s", err.Error())
	}
	return nil
}
