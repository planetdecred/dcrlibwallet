package dcrlibwallet

import (
	"github.com/asdine/storm"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/raedahgroup/dcrlibwallet/spv"

	"golang.org/x/crypto/bcrypt"
)

const (
	logFileName   = "dcrlibwallet.log"
	walletsDbName = "wallets.db"

	walletsMetadataBucketName    = "metadata"
	walletstartupPassphraseField = "startup-passphrase"
)

func (mw *MultiWallet) batchDbTransaction(dbOp func(node storm.Node) error) (err error) {
	dbTx, err := mw.db.Begin(true)
	if err != nil {
		return err
	}

	// Commit or rollback the transaction after f returns or panics.  Do not
	// recover from the panic to keep the original stack trace intact.
	panicked := true
	defer func() {
		if panicked || err != nil {
			dbTx.Rollback()
			return
		}

		err = dbTx.Commit()
	}()

	err = dbOp(dbTx)
	panicked = false
	return err
}

func (mw *MultiWallet) markWalletAsDiscoveredAccounts(walletID int) error {
	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	err := mw.db.One("ID", walletID, wallet)
	if err != nil {
		return err
	}

	wallet.HasDiscoveredAccounts = true
	err = mw.db.Save(wallet)
	if err != nil {
		return err
	}

	return nil
}

func (mw *MultiWallet) setNetworkBackend(syncer *spv.Syncer) {
	for walletID, wallet := range mw.wallets {
		if wallet.WalletOpened() {
			walletBackend := &spv.WalletBackend{
				Syncer:   syncer,
				WalletID: walletID,
			}
			wallet.internal.SetNetworkBackend(walletBackend)
		}
	}
}

func (mw *MultiWallet) verifyStartupPassphrase(startupPassphrase []byte) error {
	var startupPassphraseHash []byte
	err := mw.db.Get(walletsMetadataBucketName, walletstartupPassphraseField, &startupPassphraseHash)
	if err != nil {
		return err
	}

	if startupPassphraseHash != nil {
		// startup passphrase was set, verify
		return bcrypt.CompareHashAndPassword(startupPassphraseHash, startupPassphrase)
	}

	// startup passphrase was not previously set
	if len(startupPassphrase) > 0 {
		return errors.E(ErrInvalidPassphrase)
	}

	return nil
}
