package dcrlibwallet

import (
	// "context"
	// "fmt"
	"os"
	"path/filepath"

	"decred.org/dcrwallet/v2/errors"

	"github.com/asdine/storm"
	// "github.com/asdine/storm/q"

	bolt "go.etcd.io/bbolt"

	"github.com/planetdecred/dcrlibwallet/wallets/btc"
)

func initializeBTCWallet(rootDir, dbDriver, netType string) (*storm.DB, string, error) {
	var btcDB *storm.DB

	rootDir = filepath.Join(rootDir, netType, "btc")
	err := os.MkdirAll(rootDir, os.ModePerm)
	if err != nil {
		return btcDB, "", errors.Errorf("failed to create btc rootDir: %v", err)
	}

	err = initLogRotator(filepath.Join(rootDir, logFileName))
	if err != nil {
		return btcDB, "", errors.Errorf("failed to init btc logRotator: %v", err.Error())
	}

	btcDB, err = storm.Open(filepath.Join(rootDir, walletsDbName))
	if err != nil {
		log.Errorf("Error opening btc wallets database: %s", err.Error())
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			return btcDB, "", errors.E(ErrWalletDatabaseInUse)
		}
		return btcDB, "", errors.Errorf("error opening btc wallets database: %s", err.Error())
	}

	// init database for saving/reading wallet objects
	err = btcDB.Init(&btc.Wallet{})
	if err != nil {
		log.Errorf("Error initializing btc wallets database: %s", err.Error())
		return btcDB, "", err
	}

	return btcDB, rootDir, nil
}
