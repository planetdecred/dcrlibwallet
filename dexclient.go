package dcrlibwallet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/planetdecred/dcrlibwallet/dexdcr"
)

const (
	// CustomDexDcrWalletType is a keyword that identifies a custom dcr wallet
	// used by the DEX client.
	CustomDexDcrWalletType = "dcrlibwallet"

	// DexDcrWalletIDConfigKey is the key that holds the wallet ID value in the
	// settings map used to connect an existing dcr wallet to the DEX client.
	DexDcrWalletIDConfigKey = "walletid"
)

// DexClient represents the Decred DEX client.
type DexClient struct {
	core          *core.Core
	log           dex.Logger
	dbPath        string
	cancelCoreCtx context.CancelFunc
	isLoggedIn    bool
}

// StartDexClient readies the inbuilt DexClient for use. The client will be
// stopped when this MultiWallet instance is shutdown.
func (mw *MultiWallet) StartDexClient() (*DexClient, error) {
	if mw.dexClient == nil || mw.dexClient.core == nil {
		if err := mw.initDexClient(); err != nil {
			return nil, err
		}
	}

	if mw.dexClient.cancelCoreCtx != nil { // already started
		return mw.dexClient, nil
	}

	// Run the client core with a context that is canceled when
	// MultiWallet shuts down.
	ctx, cancel := mw.contextWithShutdownCancel()
	mw.dexClient.cancelCoreCtx = cancel
	go func() {
		mw.dexClient.core.Run(ctx)
		mw.dexClient.cancelCoreCtx()
		mw.dexClient.cancelCoreCtx = nil
	}()
	<-mw.dexClient.core.Ready()

	return mw.dexClient, nil
}

func (mw *MultiWallet) initDexClient() error {
	net := mw.NetType()
	if net == "testnet3" {
		net = "testnet"
	}
	n, err := dex.NetFromString(net)
	if err != nil {
		return err
	}

	dbPath := filepath.Join(mw.rootDir, "dex")
	log := dex.NewLogger("DEXC", log.Level(), logWriter{}, true)
	clientCore, err := core.New(&core.Config{
		DBPath: dbPath,
		Net:    n,
		Logger: log,
	})
	if err != nil {
		return fmt.Errorf("error creating dex client core: %v", err)
	}

	if mw.dexClient == nil {
		err = mw.prepareSupportForExistingDcrWallets() // first time only, errors for duplicate calls
		if err != nil {
			return fmt.Errorf("custom dcr wallet support error: %v", err)
		}
	}

	mw.dexClient = &DexClient{
		core:   clientCore,
		log:    log,
		dbPath: dbPath,
	}

	return nil
}

// DexClient returns the managed instance of a DEX client. The client must
// have been started with mw.StartDexClient().
func (mw *MultiWallet) DexClient() *DexClient {
	if mw.dexClient == nil {
		panic("DEX client is not started")
	}
	return mw.dexClient
}

// prepareSupportForExistingDcrWallets sets up the DEX client to allow using a
// custom dcr wallet as an alternative to using an rpc connection to a running
// dcrwallet instance.
func (mw *MultiWallet) prepareSupportForExistingDcrWallets() error {
	// Build a custom wallet definition with custom config options
	// for use by the dex dcr ExchangeWallet.
	customWalletConfigOpts := []*asset.ConfigOption{
		{
			Key:         DexDcrWalletIDConfigKey,
			DisplayName: "Wallet ID",
			Description: "ID of existing wallet to use",
		},
	}
	def := &asset.WalletDefinition{
		Type:        CustomDexDcrWalletType,
		Description: "Uses an existing dcrlibwallet Wallet instance instead of an rpc connection.",
		ConfigOpts:  append(customWalletConfigOpts, dexdcr.DefaultConfigOpts...),
	}

	// This function will be invoked when the DEX client needs to
	// setup a dcr ExchangeWallet; it allows us to use an existing
	// wallet instance for wallet operations instead of json-rpc.
	walletMaker := func(cfg *asset.WalletConfig, chainParams *chaincfg.Params, logger dex.Logger) (dcr.Wallet, error) {
		walletIDStr := cfg.Settings[DexDcrWalletIDConfigKey]
		walletID, err := strconv.Atoi(walletIDStr)
		if err != nil || walletID < 0 {
			return nil, fmt.Errorf("invalid wallet ID %q in settings", walletIDStr)
		}

		wallet := mw.WalletWithID(walletID)
		if wallet == nil {
			return nil, fmt.Errorf("no wallet exists with ID %q", walletIDStr)
		}
		if wallet.Internal().ChainParams().Net != chainParams.Net {
			return nil, fmt.Errorf("selected wallet is for %s network, expected %s",
				wallet.Internal().ChainParams().Name, chainParams.Name)
		}

		// Ensure the account exists.
		account := cfg.Settings["account"]
		_, err = wallet.AccountNumber(account)
		if err != nil {
			return nil, fmt.Errorf("account error: %v", err)
		}

		walletDesc := fmt.Sprintf("%q in %s", wallet.Name, wallet.dataDir)
		return dexdcr.NewSpvWallet(wallet.Internal(), walletDesc, chainParams, logger.SubLogger("DLWL")), nil
	}

	return dcr.RegisterCustomWallet(walletMaker, def)
}

// Core returns the client core that powers this DEX client.
func (d *DexClient) Core() *core.Core {
	return d.core
}

// Initialized checks if the DEX client is already initialized with a
// password.
func (d *DexClient) Initialized() bool {
	return d.core.IsInitialized()
}

// InitializeWithPassword gets the DEX client ready for use. The password
// provided will be required for future sensitive DEX operations.
func (d *DexClient) InitializeWithPassword(pass []byte) error {
	// TODO: Generate and save a 64-byte seed and pass it to InitializeClient
	// to enable dex restores if the dex db becomes corrupted. Alternatively,
	// passing nil will cause dex to generate a random seed which can be saved
	// for later dex restoration efforts.
	if err := d.core.InitializeClient(pass, nil); err != nil {
		return err
	}
	d.isLoggedIn = true
	return nil
}

// IsLoggedIn checks if the DEX client is logged in.
func (d *DexClient) IsLoggedIn() bool {
	return d.isLoggedIn
}

// Login loads and reconnects previously connected wallets and DEX servers.
// This should be done each time the DEX client is (re)started.
func (d *DexClient) Login(pass []byte) error {
	if _, err := d.core.Login(pass); err != nil {
		return err
	}
	d.isLoggedIn = true
	return nil
}

// HasWallet is true if a wallet has been added to the DEX client for the
// specified asset.
func (d *DexClient) HasWallet(assetID int32) bool {
	return d.core.WalletState(uint32(assetID)) != nil
}

// AddWallet attempts to connect or create the wallet with the provided details
// to the DEX client.
// NOTE: Before connecting a dcr wallet, first call mw.UseDcrWalletForDex to
// configure the dcr ExchangeWallet to use a custom wallet instead of the
// default rpc wallet.
func (d *DexClient) AddWallet(assetID uint32, walletType string, settings map[string]string, appPW, walletPW []byte) error {
	walletDef, err := d.walletDefinition(assetID, walletType)
	if err != nil {
		return err
	}

	// Start building the wallet config with default values.
	config := map[string]string{}
	for _, option := range walletDef.ConfigOpts {
		config[strings.ToLower(option.Key)] = fmt.Sprintf("%v", option.DefaultValue)
	}

	// User-provided settings should override defaults.
	for k, v := range settings {
		config[k] = v
	}

	return d.core.CreateWallet(appPW, walletPW, &core.WalletForm{
		AssetID: assetID,
		Config:  config,
		Type:    walletType,
	})
}

func (d *DexClient) walletDefinition(assetID uint32, walletType string) (*asset.WalletDefinition, error) {
	assetInfo, err := asset.Info(assetID)
	if err != nil {
		return nil, fmt.Errorf("unsupported asset %d", assetID)
	}

	for _, def := range assetInfo.AvailableWallets {
		if def.Type == walletType {
			return def, nil
		}
	}

	return nil, fmt.Errorf("invalid type %q for %s wallet", walletType, assetInfo.Name)
}

// DEXServerInfo attempts a connection to the DEX server at the provided
// address and returns the server info.
func (d *DexClient) DEXServerInfo(addr string, cert []byte) (*core.Exchange, error) {
	// TODO: Use DiscoverAccount instead of GetDEXConfig to enable account
	// recovery without re-paying the fee. This is only relevant when the
	// dex client supports restoring from seed. Requires a dexcPass param.
	return d.core.GetDEXConfig(addr, cert)
}

// RegisterWithDEXServer creates an account with the DEX server at the provided
// address and returns the registration result. The feeAmt may be paid from the
// specified asset wallet and the account will only be able to trade after the
// fee has received the required network confirmations. No fee is paid if this
// DEX client was initialized with a seed that has previously registered with
// the server and the fee was already paid.
func (d *DexClient) RegisterWithDEXServer(addr string, cert []byte, feeAmt int64, feeAsset int32, dexcPass []byte) (*core.RegisterResult, error) {
	feeAssetID := uint32(feeAsset)
	form := &core.RegisterForm{
		AppPass: dexcPass,
		Addr:    addr,
		Cert:    cert,
		Fee:     uint64(feeAmt),
		Asset:   &feeAssetID,
	}
	return d.core.Register(form)
}

func (d *DexClient) DEXServers() map[string]*core.Exchange {
	return d.core.Exchanges()
}

// Reset attempts to shutdown Core if it is running and if successful, deletes
// the DEX client database.
func (d *DexClient) Reset() bool {
	shutdownOk := d.shutdown(false)
	if !shutdownOk {
		return false
	}

	err := os.RemoveAll(d.dbPath)
	if err != nil {
		d.log.Warnf("DEX client reset failed: erroring deleting DEX db: %v", err)
		return false
	}
	return true
}

// shutdown causes the dex client to shutdown. If there are active orders,
// this shutdown attempt will fail unless `forceShutdown` is true. If shutdown
// succeeds, dexc will need to be restarted before it can be used.
func (d *DexClient) shutdown(forceShutdown bool) bool {
	err := d.core.Logout()
	if err != nil {
		d.log.Errorf("Unable to stop the dex client: %v", err)
		if !forceShutdown { // abort shutdown because of the error since forceShutdown != true
			return false
		}
	}

	// Cancel the ctx used to run Core.
	if d.cancelCoreCtx != nil { // in case dexc was never actually started
		d.cancelCoreCtx()
	}
	d.isLoggedIn = false
	d.core = nil // Core should be recreated before being used again.
	return true
}
