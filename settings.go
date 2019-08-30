package dcrlibwallet

import (
	"fmt"
	"path/filepath"

	"github.com/asdine/storm"
	"go.etcd.io/bbolt"
)

const (
	settingsDbFilename = "config.db"
	settingsBucketName = "settings"

	VSPHostSettingsKey = "vsp_host"
)

// initDb attempts to create/open the settings db file if it is not already open.
func (lw *LibWallet) initSettingsDb() error {
	if lw.settingsDB != nil {
		// already open
		return nil
	}

	dbPath := filepath.Join(lw.walletDataDir, settingsDbFilename)
	settingsDB, err := storm.Open(dbPath)
	if err != nil {
		if err == bolt.ErrTimeout {
			// timeout error occurs if storm fails to acquire a lock on the database file
			return fmt.Errorf("settings db is in use by another process")
		}
		return fmt.Errorf("error opening settings db store: %s", err.Error())
	}

	lw.settingsDB = settingsDB
	return nil
}

func (lw *LibWallet) SaveToSettings(key string, value interface{}) error {
	if err := lw.initSettingsDb(); err != nil {
		return err
	}

	return lw.settingsDB.Set(settingsBucketName, key, value)
}

func (lw *LibWallet) ReadFromSettings(key string, valueOut interface{}) error {
	if err := lw.initSettingsDb(); err != nil {
		return err
	}

	err := lw.settingsDB.Get(settingsBucketName, key, valueOut)
	if err != nil && err != storm.ErrNotFound {
		return err
	}
	return nil
}
