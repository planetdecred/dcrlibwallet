package dcrlibwallet

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"decred.org/dcrwallet/v2/deployments"
	"decred.org/dcrwallet/v2/errors"
	w "decred.org/dcrwallet/v2/wallet"
	"decred.org/dcrwallet/v2/walletseed"
	"github.com/asdine/storm"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/kevinburke/nacl"
	"github.com/kevinburke/nacl/secretbox"
	"golang.org/x/crypto/scrypt"

	"github.com/planetdecred/dcrlibwallet/wallets/dcr"
)

const (
	logFileName   = "dcrlibwallet.log"
	walletsDbName = "wallets.db"

	walletsMetadataBucketName    = "metadata"
	walletstartupPassphraseField = "startup-passphrase"
)

var (
	Mainnet  = chaincfg.MainNetParams().Name
	Testnet3 = chaincfg.TestNet3Params().Name
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
		return translateError(err)
	}

	// unload wallet after temporary use
	defer walletLoader.UnloadWallet()

	if onLoaded != nil {
		return onLoaded(wallet)
	}

	return nil
}

func (mw *MultiWallet) markWalletAsDiscoveredAccounts(walletID int) error {
	wallet, _ := mw.WalletWithID(walletID, "DCR")
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	log.Infof("Set discovered accounts = true for wallet %d", wallet.ID)
	wallet.HasDiscoveredAccounts = true
	err := mw.db.Save(wallet)
	if err != nil {
		return err
	}

	return nil
}

// RootDirFileSizeInBytes returns the total directory size of
// multiwallet's root directory in bytes.
func (mw *MultiWallet) RootDirFileSizeInBytes() (int64, error) {
	var size int64
	err := filepath.Walk(mw.rootDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

// DCP0001ActivationBlockHeight returns the hardcoded block height that
// the DCP0001 deployment activates at. DCP0001 specifies hard forking
// changes to the stake difficulty algorithm.
func (mw *MultiWallet) DCP0001ActivationBlockHeight() int32 {
	var activationHeight int32 = -1
	switch strings.ToLower(mw.chainParams.Name) {
	case strings.ToLower(Mainnet):
		activationHeight = deployments.DCP0001.MainNetActivationHeight
	case strings.ToLower(Testnet3):
		activationHeight = deployments.DCP0001.TestNet3ActivationHeight
	default:
	}

	return activationHeight
}

// WalletWithXPub returns the ID of the wallet that has an account with the
// provided xpub. Returns -1 if there is no such wallet.
func (mw *MultiWallet) WalletWithXPub(xpub string) (int, error) {
	ctx, cancel := mw.contextWithShutdownCancel()
	defer cancel()

	for _, w := range mw.wallets {
		if !w.WalletOpened() {
			return -1, errors.Errorf("wallet %d is not open and cannot be checked", w.ID)
		}
		accounts, err := w.Internal().Accounts(ctx)
		if err != nil {
			return -1, err
		}
		for _, account := range accounts.Accounts {
			if account.AccountNumber == dcr.ImportedAccountNumber {
				continue
			}
			acctXPub, err := w.Internal().AccountXpub(ctx, account.AccountNumber)
			if err != nil {
				return -1, err
			}
			if acctXPub.String() == xpub {
				return w.ID, nil
			}
		}
	}
	return -1, nil
}

// WalletWithSeed returns the ID of the wallet that was created or restored
// using the same seed as the one provided. Returns -1 if no wallet uses the
// provided seed.
func (mw *MultiWallet) WalletWithSeed(seedMnemonic string) (int, error) {
	if len(seedMnemonic) == 0 {
		return -1, errors.New(ErrEmptySeed)
	}

	newSeedLegacyXPUb, newSeedSLIP0044XPUb, err := deriveBIP44AccountXPubs(seedMnemonic, dcr.DefaultAccountNum, mw.chainParams)
	if err != nil {
		return -1, err
	}

	for _, wallet := range mw.wallets {
		if !wallet.WalletOpened() {
			return -1, errors.Errorf("cannot check if seed matches unloaded wallet %d", wallet.ID)
		}
		// NOTE: Existing watch-only wallets may have been created using the
		// xpub of an account that is NOT the default account and may return
		// incorrect result from the check below. But this would return true
		// if the watch-only wallet was created using the xpub of the default
		// account of the provided seed.
		usesSameSeed, err := wallet.AccountXPubMatches(dcr.DefaultAccountNum, newSeedLegacyXPUb, newSeedSLIP0044XPUb)
		if err != nil {
			return -1, err
		}
		if usesSameSeed {
			return wallet.ID, nil
		}
	}

	return -1, nil
}

// deriveBIP44AccountXPub derives and returns the legacy and SLIP0044 account
// xpubs using the BIP44 HD path for accounts: m/44'/<coin type>'/<account>'.
func deriveBIP44AccountXPubs(seedMnemonic string, account uint32, params *chaincfg.Params) (string, string, error) {
	seed, err := walletseed.DecodeUserInput(seedMnemonic)
	if err != nil {
		return "", "", err
	}
	defer func() {
		for i := range seed {
			seed[i] = 0
		}
	}()

	// Derive the master extended key from the provided seed.
	masterNode, err := hdkeychain.NewMaster(seed, params)
	if err != nil {
		return "", "", err
	}
	defer masterNode.Zero()

	// Derive the purpose key as a child of the master node.
	purpose, err := masterNode.Child(44 + hdkeychain.HardenedKeyStart)
	if err != nil {
		return "", "", err
	}
	defer purpose.Zero()

	accountXPub := func(coinType uint32) (string, error) {
		coinTypePrivKey, err := purpose.Child(coinType + hdkeychain.HardenedKeyStart)
		if err != nil {
			return "", err
		}
		defer coinTypePrivKey.Zero()
		acctPrivKey, err := coinTypePrivKey.Child(account + hdkeychain.HardenedKeyStart)
		if err != nil {
			return "", err
		}
		defer acctPrivKey.Zero()
		return acctPrivKey.Neuter().String(), nil
	}

	legacyXPUb, err := accountXPub(params.LegacyCoinType)
	if err != nil {
		return "", "", err
	}
	slip0044XPUb, err := accountXPub(params.SLIP0044CoinType)
	if err != nil {
		return "", "", err
	}

	return legacyXPUb, slip0044XPUb, nil
}

// naclLoadFromPass derives a nacl.Key from pass using scrypt.Key.
func naclLoadFromPass(pass []byte) (nacl.Key, error) {

	const N, r, p = 1 << 15, 8, 1

	hash, err := scrypt.Key(pass, nil, N, r, p, 32)
	if err != nil {
		return nil, err
	}
	return nacl.Load(EncodeHex(hash))
}

// encryptWalletSeed encrypts the seed with secretbox.EasySeal using pass.
func encryptWalletSeed(pass []byte, seed string) ([]byte, error) {
	key, err := naclLoadFromPass(pass)
	if err != nil {
		return nil, err
	}
	return secretbox.EasySeal([]byte(seed), key), nil
}

// decryptWalletSeed decrypts the encryptedSeed with secretbox.EasyOpen using pass.
func decryptWalletSeed(pass []byte, encryptedSeed []byte) (string, error) {
	key, err := naclLoadFromPass(pass)
	if err != nil {
		return "", err
	}

	decryptedSeed, err := secretbox.EasyOpen(encryptedSeed, key)
	if err != nil {
		return "", errors.New(ErrInvalidPassphrase)
	}

	return string(decryptedSeed), nil
}
