package txindex

import (
	"fmt"
	"os"

	"github.com/asdine/storm"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
	"go.etcd.io/bbolt"
)

const (
	TxBucketName             = "TxIndexInfo"
	KeyDbVersion             = "DbVersion"
	KeyMatchingWalletAddress = "MatchingWalletAddress"

	// necessary to force re-indexing if changes are made to the structure of data being stored
	// increment value if there's a change to the `txhelper.Transaction` struct
	TxDbVersion uint32 = 1
)

type DB struct {
	txDB  *storm.DB
	Close func() error
	Count func(data interface{}) (int, error)
}

type GenerateAddressFn func() (string, error)
type AddressMatchFn func(address string) (bool, error)

func Initialize(dbPath string, generateWalletAddress GenerateAddressFn, addressMatchesWallet AddressMatchFn) (*DB, error) {

	txDB, err := openDB(dbPath, generateWalletAddress)
	if err != nil {
		return nil, err
	}

	err = ensureDatabaseVersion(txDB, dbPath, generateWalletAddress)
	if err != nil {
		return nil, err
	}

	err = ensureWalletDatabaseMatch(txDB, dbPath, addressMatchesWallet, generateWalletAddress)
	if err != nil {
		return nil, err
	}

	// init database for saving/reading transaction objects
	err = txDB.Init(&txhelper.Transaction{})
	if err != nil {
		return nil, err
	}

	return &DB{
		txDB,
		txDB.Close,
		txDB.Count,
	}, nil
}

func openDB(dbPath string, generateWalletAddress func() (string, error)) (txDB *storm.DB, err error) {
	var isNewDbFile bool
	var walletAddress string

	// first check if db file exists at dbPath, if not we'll need to create it and set the matching wallet address
	_, err = os.Stat(dbPath)
	if err != nil && os.IsNotExist(err) {
		isNewDbFile = true
		walletAddress, err = generateWalletAddress()
		if err != nil {
			err = fmt.Errorf("error generating wallet address to initialize tx index db: %s", err.Error())
			return
		}
	} else if err != nil {
		err = fmt.Errorf("error checking tx index database file: %s", err.Error())
		return
	}

	txDB, err = storm.Open(dbPath)
	if err != nil {
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			err = fmt.Errorf("tx index database is in use by another process")
			return
		}
		err = fmt.Errorf("error opening tx index database: %s", err.Error())
		return
	}

	if isNewDbFile {
		err = txDB.Set(TxBucketName, KeyMatchingWalletAddress, walletAddress)
		if err == nil {
			err = txDB.Set(TxBucketName, KeyDbVersion, TxDbVersion)
		}

		if err != nil {
			err = fmt.Errorf("error initializing tx index db: %s", err.Error())
			closeAndDeleteDb(txDB, dbPath)
			return
		}
	}

	return
}
