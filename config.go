package dcrlibwallet

import (
	"github.com/asdine/storm"
)

const (
	userConfigDbFilename = "config.db"
	userConfigBucketName = "user_config"

	LogLevelConfigKey = "log_level"

	NewWalletSetUpConfigKey       = "new_wallet_set_up"
	InitialSyncCompletedConfigKey = "initial_sync_complete"
	DefaultWalletConfigKey        = "default_wallet"
	HiddenWalletPrefixConfigKey   = "hidden"

	SpendUnconfirmedConfigKey   = "spend_unconfirmed"
	CurrencyConversionConfigKey = "currency_conversion_option"

	SpendingPassphraseSecurityTypeConfigKey = "spending_security_type"
	IsStartupSecuritySetConfigKey           = "startup_security_set"
	StartupSecurityTypeConfigKey            = "startup_security_type"

	IncomingTxNotificationsConfigKey = "tx_notification_enabled"
	BeepNewBlocksConfigKey           = "beep_bop"

	SyncOnCellularConfigKey             = "always_sync"
	NetworkModeConfigKey                = "network_mode"
	SpvPersistentPeerAddressesConfigKey = "spv_peer_addresses"
	RemoteServerIPConfigKey             = "remote_server_ip"
	UserAgentConfigKey                  = "user_agent"

	LastTxHashConfigKey = "last_tx_hash"

	VSPHostConfigKey = "vsp_host"
)

func (mw *MultiWallet) SaveUserConfigValue(key string, value interface{}) error {
	return mw.configDB.Set(userConfigBucketName, key, value)
}

func (mw *MultiWallet) ReadUserConfigValue(key string, valueOut interface{}) error {
	err := mw.configDB.Get(userConfigBucketName, key, valueOut)
	if err != nil && err != storm.ErrNotFound {
		return err
	}
	return nil
}

func (mw *MultiWallet) SetBoolConfigValueForKey(key string, value bool) {
	err := mw.SaveUserConfigValue(key, value)
	if err != nil {
		log.Errorf("error setting config value: %v", err)
	}
}

func (mw *MultiWallet) SetDoubleConfigValueForKey(key string, value float64) {
	err := mw.SaveUserConfigValue(key, value)
	if err != nil {
		log.Errorf("error setting config value: %v", err)
	}
}

func (mw *MultiWallet) SetIntConfigValueForKey(key string, value int) {
	err := mw.SaveUserConfigValue(key, value)
	if err != nil {
		log.Errorf("error setting config value: %v", err)
	}
}

func (mw *MultiWallet) SetLongConfigValueForKey(key string, value int64) {
	err := mw.SaveUserConfigValue(key, value)
	if err != nil {
		log.Errorf("error setting config value: %v", err)
	}
}

func (mw *MultiWallet) SetStringConfigValueForKey(key, value string) {
	err := mw.SaveUserConfigValue(key, value)
	if err != nil {
		log.Errorf("error setting config value: %v", err)
	}
}

func (mw *MultiWallet) ReadBoolConfigValueForKey(key string) (valueOut bool) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		log.Errorf("error reading config value: %v", err)
	}
	return
}

func (mw *MultiWallet) ReadDoubleConfigValueForKey(key string) (valueOut float64) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		log.Errorf("error reading config value: %v", err)
	}
	return
}

func (mw *MultiWallet) ReadIntConfigValueForKey(key string) (valueOut int) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		log.Errorf("error reading config value: %v", err)
	}
	return
}

func (mw *MultiWallet) ReadLongConfigValueForKey(key string) (valueOut int64) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		log.Errorf("error reading config value: %v", err)
	}
	return
}

func (mw *MultiWallet) ReadStringConfigValueForKey(key string) (valueOut string) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		log.Errorf("error reading config value: %v", err)
	}
	return
}
