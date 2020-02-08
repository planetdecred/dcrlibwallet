package dcrlibwallet

import (
	"context"

	"github.com/asdine/storm"
	"github.com/decred/dcrwallet/errors/v2"
	w "github.com/decred/dcrwallet/wallet/v3"
	"github.com/raedahgroup/dcrlibwallet/spv"
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

func (mw *MultiWallet) loadWalletTemporarily(ctx context.Context, walletDataDir, walletPublicPass string,
	onLoaded func(*w.Wallet) error) error {

	if walletPublicPass == "" {
		walletPublicPass = w.InsecurePubPassphrase
	}

	// initialize the wallet loader
	walletLoader := initWalletLoader(mw.chainParams, walletDataDir, mw.dbDriver)

	// open the wallet to get ready for temporary use
	wallet, err := walletLoader.OpenExistingWallet(ctx, []byte(walletPublicPass))
	if err != nil {
		return err
	}

	// unload wallet after temporary use
	defer walletLoader.UnloadWallet()

	if onLoaded != nil {
		return onLoaded(wallet)
	}

	return nil
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
