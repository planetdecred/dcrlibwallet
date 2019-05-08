package dcrlibwallet

import (
	rand "crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/decred/dcrlnd/aezeed"
	channeldb "github.com/decred/dcrlnd/channeldb"
	keychain "github.com/decred/dcrlnd/keychain"
	dcrwallet "github.com/decred/dcrlnd/lnwallet/dcrwallet"
	loader "github.com/decred/dcrwallet/loader"
	"github.com/decred/dcrwallet/netparams"
	wallet "github.com/decred/dcrwallet/wallet/v2"
	txrules "github.com/decred/dcrwallet/wallet/txrules"
	"github.com/raedahgroup/dcrlibwallet/utils"
)

const (
	defaultGraphSubDirname = "graph"
	defaultRecoveryWindow int32 = 250
)

type LibLnWallet struct {
	activeNet        *netparams.Params
	channeldb        *channeldb.DB
	registeredChains *chainRegistry
	walletDataDir    string
}

func NewLibLnWallet(homeDir, dbDriver, netType string) (*LibLnWallet, error) {
	activeNet := utils.NetParams(netType)
	if activeNet == nil {
		return nil, fmt.Errorf("unsupported network type: %s", netType)
	}

	walletDataDir := filepath.Join(homeDir, activeNet.Name)

	// Create the network-segmented directory for the channel database.
	graphDir := filepath.Join(homeDir, defaultGraphSubDirname, normalizeNetwork(activeNet.Name))

	// Open the channeldb, which is dedicated to storing channel, and
	// network related metadata.
	chanDB, err := channeldb.Open(graphDir)
	if err != nil {
		log.Errorf("unable to open channeldb: %v", err)
		return nil, err
	}

	// ctx := context.Background()
	// ctx, cancel := context.WithCancel(ctx)

	lnw := &LibLnWallet{
		activeNet:        activeNet,
		channeldb:        chanDB,
		registeredChains: newChainRegistry(),
		walletDataDir:    walletDataDir,
	}

	return lnw, nil
}

func (lnw *LibLnWallet) GenerateSeed() (string, error) {
	netDir := dcrwallet.NetworkDir(lnw.walletDataDir, lnw.activeNet.Params)
	loader := loader.NewLoader(lnw.activeNet.Params, netDir,
		&loader.StakeOptions{}, wallet.DefaultGapLimit, false,
		txrules.DefaultRelayFeePerKb.ToCoin(), wallet.DefaultAccountGapLimit,
		false)

	walletExists, err := loader.WalletExists()
	if err != nil {
		return "", err
	}

	if walletExists {
		return "", fmt.Errorf("wallet already exists")
	}

	var entropy [aezeed.EntropySize]byte

	// generate a fresh new set of bytes to use as entropy to generate the seed.
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}

	//create a new cipher seed instance.
	cipherSeed, err := aezeed.New(keychain.KeyDerivationVersion, &entropy, time.Now())
	if err != nil {
		return "", err
	}

	// With our raw cipher seed obtained, we'll convert it into an encoded
	// mnemonic using the default pass phrase.
	emptyPass := []byte{}
	mnemonic, err := cipherSeed.ToMnemonic(emptyPass)
	if err != nil {
		return "", err
	}

	seedWords := strings.Join(mnemonic[:], " ")

	return seedWords, nil
}

func (lnw *LibLnWallet) VerifySeed(seedWords string) bool {
	// We'll trim off extra spaces, and ensure the mnemonic is all
	// lower case, then populate our request.
	seedWords = strings.TrimSpace(seedWords)
	seedWords = strings.ToLower(seedWords)

	cipherSeedMnemonic := strings.Split(seedWords, " ")
	if len(cipherSeedMnemonic) != 24 {
		return false
	}

	var mnemonic aezeed.Mnemonic
	copy(mnemonic[:], cipherSeedMnemonic[:])

	// If we're unable to map it back into the ciphertext, then either the
	// mnemonic is wrong, or the passphrase is wrong.
	emptyPass := []byte{}
	_, err := mnemonic.ToCipherSeed(emptyPass)
	if err != nil {
		return false
	}

	return true
}

func (lnw *LibLnWallet) InitWallet(walletPassword, seedMnemonic string) error {
	if len(walletPassword) < 8 {
		return errors.New("Password length should be >8")
	}

	privatePassphrase := []byte(walletPassword)

	// We'll then open up the directory that will be used to store the
	// wallet's files so we can check if the wallet already exists. This
	// loader is only used for this check and should not leak to the
	// outside.
	netDir := dcrwallet.NetworkDir(lnw.walletDataDir, lnw.activeNet.Params)
	loader := loader.NewLoader(lnw.activeNet.Params, netDir,
		&loader.StakeOptions{}, wallet.DefaultGapLimit, false,
		txrules.DefaultRelayFeePerKb.ToCoin(), wallet.DefaultAccountGapLimit,
		false)

	walletExists, err := loader.WalletExists()
	if err != nil {
		return err
	}

	// If the wallet already exists, then we'll exit early as we can't
	// create the wallet if it already exists!
	if walletExists {
		return fmt.Errorf("wallet already exists")
	}

	// At this point, we know that the wallet doesn't already exist. So
	// we'll map the user provided aezeed and passphrase into a decoded
	// cipher seed instance.
	var mnemonic aezeed.Mnemonic
	copy(mnemonic[:], strings.Split(seedMnemonic, " "))

	// If we're unable to map it back into the ciphertext, then either the
	// mnemonic is wrong, or the passphrase is wrong.
	emptyPass := []byte{}
	cipherSeed, err := mnemonic.ToCipherSeed(emptyPass)
	if err != nil {
		return err
	}
	if cipherSeed.InternalVersion != keychain.KeyDerivationVersion {
		return fmt.Errorf("invalid internal seed version %v, current version is %v", cipherSeed.InternalVersion, keychain.KeyDerivationVersion)
	}

	birthday := cipherSeed.BirthdayTime()
	newWallet, err := loader.CreateNewWallet([]byte(wallet.InsecurePubPassphrase), privatePassphrase, cipherSeed.Entropy[:])
	if err != nil {
		// Don't leave the file open in case the new wallet
		// could not be created for whatever reason.
		if err := loader.UnloadWallet(); err != nil {
			log.Errorf("Could not unload new "+
				"wallet: %v", err)
		}
		return err
	}

	_, _, err = newChainControl(
		lnw.channeldb, privatePassphrase, []byte(wallet.InsecurePubPassphrase), birthday,
		newWallet, loader, lnw.walletDataDir, lnw.activeNet, lnw.registeredChains, []byte("cert"),
	)

	return nil
}
