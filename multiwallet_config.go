package dcrlibwallet

import (
	"github.com/asdine/storm"
)

const (
	userConfigBucketName = "user_config"

	LogLevelConfigKey = "log_level"

	SpendUnconfirmedConfigKey   = "spend_unconfirmed"
	CurrencyConversionConfigKey = "currency_conversion_option"

	IsStartupSecuritySetConfigKey = "startup_security_set"
	StartupSecurityTypeConfigKey  = "startup_security_type"
	UseFingerprintConfigKey       = "use_fingerprint"

	IncomingTxNotificationsConfigKey = "tx_notification_enabled"
	BeepNewBlocksConfigKey           = "beep_new_blocks"

	SyncOnCellularConfigKey             = "always_sync"
	NetworkModeConfigKey                = "network_mode"
	SpvPersistentPeerAddressesConfigKey = "spv_peer_addresses"
	UserAgentConfigKey                  = "user_agent"

	LastTxHashConfigKey = "last_tx_hash"

	VSPHostConfigKey = "vsp_host"

	PassphraseTypePin  int32 = 0
	PassphraseTypePass int32 = 1
)

type configSaveFn = func(key string, value interface{}) error
type configReadFn = func(key string, valueOut interface{}) error

func (mw *MultiWallet) walletConfigSetFn(walletID int) configSaveFn {
	return func(key string, value interface{}) error {
		walletUniqueKey := WalletUniqueConfigKey(walletID, key)
		return mw.db.Set(userConfigBucketName, walletUniqueKey, value)
	}
}

func (mw *MultiWallet) walletConfigReadFn(walletID int) configReadFn {
	return func(key string, valueOut interface{}) error {
		walletUniqueKey := WalletUniqueConfigKey(walletID, key)
		return mw.db.Get(userConfigBucketName, walletUniqueKey, valueOut)
	}
}

//SaveUserConfigValue saves config value name for key
func (mw *MultiWallet) SaveUserConfigValue(key string, value interface{}) {
	err := mw.db.Set(userConfigBucketName, key, value)
	if err != nil {
		log.Errorf("error setting config value for key: %s, error: %v", key, err)
	}
}

// ReadUserConfigValue retrieves the raw value for config key.
func (mw *MultiWallet) ReadUserConfigValue(key string, valueOut interface{}) error {
	err := mw.db.Get(userConfigBucketName, key, valueOut)
	if err != nil && err != storm.ErrNotFound {
		log.Errorf("error reading config value for key: %s, error: %v", key, err)
	}
	return err
}

// DeleteUserConfigValueForKey deletes a key from the bucket.
func (mw *MultiWallet) DeleteUserConfigValueForKey(key string) {
	err := mw.db.Delete(userConfigBucketName, key)
	if err != nil {
		log.Errorf("error deleting config value for key: %s, error: %v", key, err)
	}
}

// ClearConfig drops a config bucket name.
func (mw *MultiWallet) ClearConfig() {
	err := mw.db.Drop(userConfigBucketName)
	if err != nil {
		log.Errorf("error deleting config bucket: %v", err)
	}
}

// SetBoolConfigValueForKey sets bool config value for key.
func (mw *MultiWallet) SetBoolConfigValueForKey(key string, value bool) {
	mw.SaveUserConfigValue(key, value)
}

// SetDoubleConfigValueForKey sets float64 config value for key.
func (mw *MultiWallet) SetDoubleConfigValueForKey(key string, value float64) {
	mw.SaveUserConfigValue(key, value)
}

// SetIntConfigValueForKey sets an int config value for key.
func (mw *MultiWallet) SetIntConfigValueForKey(key string, value int) {
	mw.SaveUserConfigValue(key, value)
}

// SetInt32ConfigValueForKey sets an int32 config value for key.
func (mw *MultiWallet) SetInt32ConfigValueForKey(key string, value int32) {
	mw.SaveUserConfigValue(key, value)
}

// SetLongConfigValueForKey sets an int64 config value for key.
func (mw *MultiWallet) SetLongConfigValueForKey(key string, value int64) {
	mw.SaveUserConfigValue(key, value)
}

// SetStringConfigValueForKey sets a string config value for key.
func (mw *MultiWallet) SetStringConfigValueForKey(key, value string) {
	mw.SaveUserConfigValue(key, value)
}

// ReadBoolConfigValueForKey reads the bool config value for key.
func (mw *MultiWallet) ReadBoolConfigValueForKey(key string, defaultValue bool) (valueOut bool) {
	if err := mw.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadDoubleConfigValueForKey reads the float64 config value for key.
func (mw *MultiWallet) ReadDoubleConfigValueForKey(key string, defaultValue float64) (valueOut float64) {
	if err := mw.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadIntConfigValueForKey reads the int config value for key.
func (mw *MultiWallet) ReadIntConfigValueForKey(key string, defaultValue int) (valueOut int) {
	if err := mw.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadInt32ConfigValueForKey reads the int32 config value for key.
func (mw *MultiWallet) ReadInt32ConfigValueForKey(key string, defaultValue int32) (valueOut int32) {
	if err := mw.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadLongConfigValueForKey reads the int64 config value for key.
func (mw *MultiWallet) ReadLongConfigValueForKey(key string, defaultValue int64) (valueOut int64) {
	if err := mw.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadStringConfigValueForKey reads the string config value for key.
func (mw *MultiWallet) ReadStringConfigValueForKey(key string) (valueOut string) {
	mw.ReadUserConfigValue(key, &valueOut)
	return
}
