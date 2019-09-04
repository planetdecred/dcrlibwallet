package dcrlibwallet

import (
	"github.com/asdine/storm"
)

const (
	settingsDbFilename = "config.db"
	settingsBucketName = "settings"

	AppDataDir                 = "app_data_dir"
	SpvPersistentPeerAddresses = "spv_peer_addresses"
)

func (lw *LibWallet) SaveToSettings(key string, value interface{}) error {
	return lw.settingsDB.Set(settingsBucketName, key, value)
}

func (lw *LibWallet) ReadFromSettings(key string, valueOut interface{}) error {
	err := lw.settingsDB.Get(settingsBucketName, key, valueOut)
	if err != nil && err != storm.ErrNotFound {
		return err
	}
	return nil
}
