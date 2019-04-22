package txindex

import (
	"fmt"

	"github.com/asdine/storm"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
	"go.etcd.io/bbolt"
	"os"
)

const (
	TxBucketName = "TxIndexInfo"
	KeyEndBlock  = "EndBlock"
	KeyDbVersion = "DbVersion"

	TxDbVersion uint32    = 1 // necessary to force re-indexing if changes are made to the structure of data being stored
	MaxReOrgBlocks = 6
)

type DB struct {
	db *storm.DB
}

func Initialize(dbPath string) (*DB, error) {
	txDB, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}

	// check if database is out of date and delete it
	var currentDbVersion uint32
	err = txDB.Get(TxBucketName, KeyDbVersion, &currentDbVersion)
	if err != nil && err != storm.ErrNotFound {
		return nil, fmt.Errorf("error checking tx index database version: %s", err.Error())
	}

	if currentDbVersion != TxDbVersion {
		if err = txDB.Close(); err == nil {
			err = os.RemoveAll(dbPath)
		}
		if err != nil {
			// error closing db or deleting db file
			return nil, fmt.Errorf("error deleting outdated tx index database: %s", err.Error())
		}

		// reopen db
		txDB, err = openDB(dbPath)
		if err != nil {
			return nil, err
		}
	}

	// init database for saving/reading transaction objects
	err = txDB.Init(&txhelper.Transaction{})
	if err != nil {
		return nil, err
	}

	return &DB{
		db: txDB,
	}, nil
}

func openDB(dbPath string) (*storm.DB, error) {
	txDB, err := storm.Open(dbPath)
	if err != nil {
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			return nil, fmt.Errorf("tx index database is in use by another process")
		}
		return nil, fmt.Errorf("error opening tx index database: %s", err.Error())
	}
	return txDB, nil
}