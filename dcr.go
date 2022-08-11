package dcrlibwallet

import (
	"os"
	"path/filepath"

	"decred.org/dcrwallet/v2/errors"

	"github.com/asdine/storm"

	bolt "go.etcd.io/bbolt"

	"github.com/planetdecred/dcrlibwallet/wallets/dcr"
)

func initializeDCRWallet(rootDir, dbDriver, netType string) (*storm.DB, string, error) {
	var mwDB *storm.DB

	rootDir = filepath.Join(rootDir, netType, "dcr")
	err := os.MkdirAll(rootDir, os.ModePerm)
	if err != nil {
		return mwDB, "", errors.Errorf("failed to create dcr rootDir: %v", err)
	}

	err = initLogRotator(filepath.Join(rootDir, logFileName))
	if err != nil {
		return mwDB, "", errors.Errorf("failed to init dcr logRotator: %v", err.Error())
	}

	mwDB, err = storm.Open(filepath.Join(rootDir, walletsDbName))
	if err != nil {
		log.Errorf("Error opening dcr wallets database: %s", err.Error())
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			return mwDB, "", errors.E(ErrWalletDatabaseInUse)
		}
		return mwDB, "", errors.Errorf("error opening dcr wallets database: %s", err.Error())
	}

	// init database for saving/reading wallet objects
	err = mwDB.Init(&dcr.Wallet{})
	if err != nil {
		log.Errorf("Error initializing wallets database: %s", err.Error())
		return mwDB, "", err
	}

	// init database for saving/reading proposal objects
	err = mwDB.Init(&dcr.Proposal{})
	if err != nil {
		log.Errorf("Error initializing wallets database: %s", err.Error())
		return mwDB, "", err
	}

	return mwDB, rootDir, nil
}
