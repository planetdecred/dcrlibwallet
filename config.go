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
	IsStartupSecurityCustom                 = "custom_startup_security"
	StartupSecurityTypeConfigKey            = "startup_security_type"

	IncomingTxNotificationsConfigKey = "tx_notification_enabled"
	BeepNewBlocksConfigKey           = "beep_new_blocks"
	UseBiometricAuthConfigKey        = "use_biometric_auth" // temp

	SyncOnCellularConfigKey             = "always_sync"
	NetworkModeConfigKey                = "network_mode" // TODO: remove
	SpvPersistentPeerAddressesConfigKey = "spv_peer_addresses"
	RemoteServerIPConfigKey             = "remote_server_ip" // TODO: remove
	UserAgentConfigKey                  = "user_agent"

	LastTxHashConfigKey = "last_tx_hash"

	VSPHostConfigKey = "vsp_host"
)

func (mw *MultiWallet) SaveUserConfigValue(key string, value interface{}) error {
	return mw.configDB.Set(userConfigBucketName, key, value)
}

func (mw *MultiWallet) ReadUserConfigValue(key string, valueOut interface{}) error {
	err := mw.configDB.Get(userConfigBucketName, key, valueOut)
	if err != nil {
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

func (mw *MultiWallet) SetInt32ConfigValueForKey(key string, value int32) {
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

func (mw *MultiWallet) ReadBoolConfigValueForKey(key string, defaultValue bool) (valueOut bool) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		if err == storm.ErrNotFound {
			valueOut = defaultValue
			return
		}
		log.Errorf("error reading config value: %v", err)
	}

	return
}

func (mw *MultiWallet) ReadDoubleConfigValueForKey(key string, defaultValue float64) (valueOut float64) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		if err == storm.ErrNotFound {
			valueOut = defaultValue
			return
		}
		log.Errorf("error reading config value: %v", err)
	}
	return
}

func (mw *MultiWallet) ReadIntConfigValueForKey(key string, defaultValue int) (valueOut int) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		if err == storm.ErrNotFound {
			valueOut = defaultValue
			return
		}
		log.Errorf("error reading config value: %v", err)
	}
	return
}

func (mw *MultiWallet) ReadInt32ConfigValueForKey(key string, defaultValue int32) (valueOut int32) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		if err == storm.ErrNotFound {
			valueOut = defaultValue
			return
		}
		log.Errorf("error reading config value: %v", err)
	}
	return
}

func (mw *MultiWallet) ReadLongConfigValueForKey(key string, defaultValue int64) (valueOut int64) {
	err := mw.ReadUserConfigValue(key, &valueOut)
	if err != nil {
		if err == storm.ErrNotFound {
			valueOut = defaultValue
			return
		}
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
