package txindex

import (
	"fmt"
	"os"

	"github.com/asdine/storm"
)

// ensureDatabaseVersion checks the version of the existing db against `TxDbVersion`.
// If there's a difference, the current tx index db file is deleted and a new one created.
func ensureDatabaseVersion(txDB *storm.DB, dbPath string, generateWalletAddress func() (string, error)) error {
	var currentDbVersion uint32
	err := txDB.Get(TxBucketName, KeyDbVersion, &currentDbVersion)
	if err != nil && err != storm.ErrNotFound {
		return fmt.Errorf("error checking tx index database version: %s", err.Error())
	}

	if currentDbVersion != TxDbVersion {
		if err = os.RemoveAll(dbPath); err != nil {
			return fmt.Errorf("error deleting outdated tx index database: %s", err.Error())
		}

		// reopen db
		txDB, err = openDB(dbPath, generateWalletAddress)
		if err != nil {
			return err
		}
	}

	return nil
}

// ensureWalletDatabaseMatch checks if the wallet address saved in txDb belongs to the loaded wallet.
// If it does not, the current tx index db file is deleted and a new one created.
func ensureWalletDatabaseMatch(txDB *storm.DB, dbPath string, addressMatchesWallet AddressMatchFn,
	generateWalletAddress GenerateAddressFn) error {

	var walletAddressForCurrentTxDb string
	err := txDB.Get(TxBucketName, KeyMatchingWalletAddress, &walletAddressForCurrentTxDb)
	if err != nil && err != storm.ErrNotFound {
		return fmt.Errorf("error checking tx/wallet db match: %s", err.Error())
	}

	txDbMatchesWalletDb, err := addressMatchesWallet(walletAddressForCurrentTxDb)
	if err != nil {
		return fmt.Errorf("error checking tx/wallet db match: %s", err.Error())
	}

	if !txDbMatchesWalletDb {
		if err = os.RemoveAll(dbPath); err != nil {
			return fmt.Errorf("error deleting outdated tx index database: %s", err.Error())
		}

		// reopen db
		txDB, err = openDB(dbPath, generateWalletAddress)
		if err != nil {
			return err
		}
	}

	return nil
}
