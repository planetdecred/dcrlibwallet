package dcrlibwallet

import (
	"decred.org/dcrwallet/errors"
	"github.com/asdine/storm"
)

const (
	AccountMixerConfigSet      = "account_mixer_config_set"
	AccountMixerMixedAccount   = "account_mixer_mixed_account"
	AccountMixerUnmixedAccount = "account_mixer_unmixed_account"
)

func (wallet *Wallet) SaveUserConfigValue(key string, value interface{}) {
	if wallet.setUserConfigValue == nil {
		log.Errorf("call wallet.prepare before setting wallet config values")
		return
	}

	err := wallet.setUserConfigValue(key, value)
	if err != nil {
		log.Errorf("error setting config value for key: %s, error: %v", key, err)
	}
}

func (wallet *Wallet) ReadUserConfigValue(key string, valueOut interface{}) error {
	if wallet.setUserConfigValue == nil {
		log.Errorf("call wallet.prepare before reading wallet config values")
		return errors.New(ErrFailedPrecondition)
	}

	err := wallet.readUserConfigValue(false, key, valueOut)
	if err != nil && err != storm.ErrNotFound {
		log.Errorf("error reading config value for key: %s, error: %v", key, err)
	}
	return err
}

func (wallet *Wallet) SetBoolConfigValueForKey(key string, value bool) {
	wallet.SaveUserConfigValue(key, value)
}

func (wallet *Wallet) SetDoubleConfigValueForKey(key string, value float64) {
	wallet.SaveUserConfigValue(key, value)
}

func (wallet *Wallet) SetIntConfigValueForKey(key string, value int) {
	wallet.SaveUserConfigValue(key, value)
}

func (wallet *Wallet) SetInt32ConfigValueForKey(key string, value int32) {
	wallet.SaveUserConfigValue(key, value)
}

func (wallet *Wallet) SetLongConfigValueForKey(key string, value int64) {
	wallet.SaveUserConfigValue(key, value)
}

func (wallet *Wallet) SetStringConfigValueForKey(key, value string) {
	wallet.SaveUserConfigValue(key, value)
}

func (wallet *Wallet) ReadBoolConfigValueForKey(key string, defaultValue bool) (valueOut bool) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

func (wallet *Wallet) ReadDoubleConfigValueForKey(key string, defaultValue float64) (valueOut float64) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

func (wallet *Wallet) ReadIntConfigValueForKey(key string, defaultValue int) (valueOut int) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

func (wallet *Wallet) ReadInt32ConfigValueForKey(key string, defaultValue int32) (valueOut int32) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

func (wallet *Wallet) ReadLongConfigValueForKey(key string, defaultValue int64) (valueOut int64) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

func (wallet *Wallet) ReadStringConfigValueForKey(key string, defaultValue string) (valueOut string) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}
