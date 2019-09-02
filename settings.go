package dcrlibwallet

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/asdine/storm"
	bolt "go.etcd.io/bbolt"
)

const (
	settingsDb         = "config.db"
	settingsBucketName = "SettingsIndex"
)

// initSettingsDb checks if settingsDb (config.db) exists.
// If it exists, it attempts to open it. If it doesn't, it atempts
// to create a new settings DB and save default settings.
func (lw *LibWallet) initSettingsDb() error {
	if lw.dbExists() {
		if lw.settingsDB != nil {
			// already open
			return nil
		} else {
			err := fmt.Errorf("%s is not open", settingsDb)
			return err
		}
	} else {
		log.Infof("Settings DB " + settingsDb + " doesn't exist.\n")
		//create and set default settings in settingsDb
		dbPath := filepath.Join(lw.walletDataDir, settingsDb)
		log.Infof("Creating DB " + settingsDb)
		settingsDb, err := storm.Open(dbPath)

		if err != nil {
			if err == bolt.ErrTimeout {
				log.Errorf("settingsDb is in use by another process")
			}
			log.Errorf("Error opening database: %s", err.Error())
		}

		lw.settingsDB = settingsDb
		err = lw.setDefaultSettings(settingsDb)
		if err != nil {
			return err
		}
	}
	return nil
}

func (lw *LibWallet) setDefaultSettings(db *storm.DB) error {
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
		err := db.Set(settingsBucketName, key, value)
		if err != nil {
			err = fmt.Errorf("error initializing value %s in %s", value, settingsDb)
			return err
		}
	}

	return nil
}

func (lw *LibWallet) ReadValue(key string, valueOut interface{}) error {
	defer lw.settingsDB.Close()
	if err := lw.initSettingsDb(); err != nil {
		return err
	}
	err := lw.settingsDB.Get(settingsBucketName, key, valueOut)
	if err != nil && err != storm.ErrNotFound {
		return err
	}
	return nil
}

func (lw *LibWallet) WriteValue(key string, value interface{}) error {
	defer lw.settingsDB.Close()
	if err := lw.initSettingsDb(); err != nil {
		return err
	}
	return lw.settingsDB.Set(settingsBucketName, key, value)
}

func (lw *LibWallet) dbExists() bool {
	if _, err := os.Stat(settingsDb); os.IsExist(err) {
		return false
	}
	return true
}
