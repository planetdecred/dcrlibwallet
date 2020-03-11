package dcrlibwallet

import (
	"github.com/asdine/storm"
	"github.com/decred/dcrwallet/errors/v2"
)

// SaveUserConfigValue saves the provided key-value pair to a config database.
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

// ReadUserConfigValue returns the saved key-value pair in a config database.
func (wallet *Wallet) ReadUserConfigValue(key string, valueOut interface{}) error {
	if wallet.setUserConfigValue == nil {
		log.Errorf("call wallet.prepare before reading wallet config values")
		return errors.New(ErrFailedPrecondition)
	}

	err := wallet.readUserConfigValue(key, valueOut)
	if err != nil && err != storm.ErrNotFound {
		log.Errorf("error reading config value for key: %s, error: %v", key, err)
	}
	return err
}

// SetBoolConfigValueForKey sets a bool value
// for key-value pair in a config database.
func (wallet *Wallet) SetBoolConfigValueForKey(key string, value bool) {
	wallet.SaveUserConfigValue(key, value)
}

// SetDoubleConfigValueForKey sets a float64 value
// for key-value pair in a config database.
func (wallet *Wallet) SetDoubleConfigValueForKey(key string, value float64) {
	wallet.SaveUserConfigValue(key, value)
}

// SetIntConfigValueForKey sets an int value
// for key-value pair in a config database.
func (wallet *Wallet) SetIntConfigValueForKey(key string, value int) {
	wallet.SaveUserConfigValue(key, value)
}

// SetInt32ConfigValueForKey sets an int32 value
// for key-value pair in a config database.
func (wallet *Wallet) SetInt32ConfigValueForKey(key string, value int32) {
	wallet.SaveUserConfigValue(key, value)
}

// SetLongConfigValueForKey sets an int64 value
// for key-value pair in a config database.
func (wallet *Wallet) SetLongConfigValueForKey(key string, value int64) {
	wallet.SaveUserConfigValue(key, value)
}

// SetStringConfigValueForKey sets a string value
// for key-value pair in a config database.
func (wallet *Wallet) SetStringConfigValueForKey(key, value string) {
	wallet.SaveUserConfigValue(key, value)
}

// ReadBoolConfigValueForKey returns the bool value
// for  key-value pair from a config database.
func (wallet *Wallet) ReadBoolConfigValueForKey(key string, defaultValue bool) (valueOut bool) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadDoubleConfigValueForKey returns a float64 value
// for key-value pair from a config database.
func (wallet *Wallet) ReadDoubleConfigValueForKey(key string, defaultValue float64) (valueOut float64) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadIntConfigValueForKey returns an int value
// for a key-value pair from a config database.
func (wallet *Wallet) ReadIntConfigValueForKey(key string, defaultValue int) (valueOut int) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadInt32ConfigValueForKey returns an int32 value
// for a key-value pair from a config database.
func (wallet *Wallet) ReadInt32ConfigValueForKey(key string, defaultValue int32) (valueOut int32) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadLongConfigValueForKey returns an int64 value
// for a key-value pair from a config database.
func (wallet *Wallet) ReadLongConfigValueForKey(key string, defaultValue int64) (valueOut int64) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}

// ReadStringConfigValueForKey returns the string value
// for a key-value pair from a config database.
func (wallet *Wallet) ReadStringConfigValueForKey(key string, defaultValue string) (valueOut string) {
	if err := wallet.ReadUserConfigValue(key, &valueOut); err == storm.ErrNotFound {
		valueOut = defaultValue
	}
	return
}
