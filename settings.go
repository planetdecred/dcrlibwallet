package dcrlibwallet

import (
	"fmt"
	"os"

	"github.com/asdine/storm"
	bolt "go.etcd.io/bbolt"
)

const (
	DbName       = "settings.db"
	TxBucketName = "SettingsIndex"
)

func InitSettings() {
	var isNewDbFile bool
	txDb, err := dbExists()
	if err != nil {
		isNewDbFile = true
		log.Infof("Settings DB " + DbName + " doesn't exist.\n")
	}

	if isNewDbFile {
		log.Infof("Creating DB " + DbName)
		defer txDb.Close()
		_, err = initializeDB(txDb)
		if err != nil {
			return
		}
		log.Infof("Default settings set in %s", DbName)
	}
}

func initializeDB(db *storm.DB) (*storm.DB, error) {
	config := map[string]interface{}{
		"new_wallet_set_up":     true,
		"initial_sync_complete": false,
		"startup_security_set":  false,
		//"StartupSecurityType": ,
		//"SpendingPassphraseSecurityType": ,
		"default_wallet":           0,
		"hidden":                   false,
		"always_sync":              false,
		"pref_peer_ip":             " ",
		"pref_server_ip":           "",
		"pref_spend_unconfirmed":   false,
		"pref_notification_switch": false,
		"network_mode":             0,
		"last_tx_hash":             "",
		//"currency_conversion_option": ,
	}

	for key, value := range config {
		err := db.Set(TxBucketName, key, value)
		if err != nil {
			err = fmt.Errorf("error initializing value %s in %s", value, DbName)
			return nil, err
		}
	}
	return db, nil
}

func ReadValue(key string) (val interface{}, err error) {
	txDb, err := dbExists()
	if err != nil {
		fmt.Println("Settings db doesn't exist")
		return nil, err
	}

	defer txDb.Close()
	err = txDb.Get(TxBucketName, key, &val)
	if err != nil {
		return nil, err
	}

	return val, nil
}

func WriteValue(key, value string) error{
	txDb, err := dbExists()
	if err != nil {
		fmt.Println("Settings db doesn't exist")
		return err
	}

	defer txDb.Close()
	err = txDb.Set(TxBucketName, key, value)
	if err != nil {
		fmt.Errorf("Could not write to settings db; %s", err.Error())
		return err
	}
	return nil
}

func dbExists() (*storm.DB, error) {
	if _, err := os.Stat(DbName); os.IsNotExist(err) {
		return nil, err
	}
	txDb, err := storm.Open(DbName)
	if err != nil {
		defer txDb.Close()
		if err != bolt.ErrTimeout {
			fmt.Errorf("tx index database is in use by another process")
		}
		fmt.Errorf("Error opening database: %s", err.Error())
		return nil, err
	}

	return txDb, nil
}
