package dcr

import (
	"decred.org/dcrwallet/v2/errors"
)

func (wallet *Wallet) markWalletAsDiscoveredAccounts() error {
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	log.Infof("Set discovered accounts = true for wallet %d", wallet.ID)
	wallet.HasDiscoveredAccounts = true
	err := wallet.db.Save(wallet)
	if err != nil {
		return err
	}

	return nil
}
