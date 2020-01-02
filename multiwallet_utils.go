package dcrlibwallet

import (
	"github.com/asdine/storm"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/raedahgroup/dcrlibwallet/internal/snacl"
)

const (
	logFileName   = "dcrlibwallet.log"
	walletsDbName = "wallets.db"

	walletsMetadataBucketName              = "metadata"
	walletsMetadataMasterPubKeyParamsField = "masterpub-params"
)

// ScryptOptions is used to hold the scrypt parameters needed when deriving new
// passphrase keys.
type ScryptOptions struct {
	N, R, P int
}

// defaultScryptOptions is the default options used with scrypt.
var defaultScryptOptions = ScryptOptions{
	N: 262144, // 2^18
	R: 8,
	P: 1,
}

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

func (mw *MultiWallet) generateAndSavePubEncKey(pubPass []byte, dbNode storm.Node) error {
	publicEncryptionKey, err := snacl.NewSecretKey(&pubPass, defaultScryptOptions.N,
		defaultScryptOptions.R, defaultScryptOptions.P)
	if err != nil {
		return errors.E("create public encryption key error: %v", err)
	}

	err = dbNode.Set(walletsMetadataBucketName, walletsMetadataMasterPubKeyParamsField,
		publicEncryptionKey.Marshal())
	if err != nil {
		return errors.E("create public encryption key error: %v", err)
	}

	mw.publicEncryptionKey = publicEncryptionKey
	return nil
}

func (mw *MultiWallet) verifyPublicPassphrase(pubPass []byte) error {
	mw.publicEncryptionKey.Zero()
	return mw.publicEncryptionKey.DeriveKey(&pubPass)
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
