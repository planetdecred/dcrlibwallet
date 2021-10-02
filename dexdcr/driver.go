package dexdcr

import (
	"encoding/binary"
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v2/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

const (
	// defaultFee is the default value for the fallbackfee.
	defaultFee = 20
	// defaultFeeRateLimit is the default value for the feeratelimit.
	defaultFeeRateLimit = 100
	// defaultRedeemConfTarget is the default redeem transaction confirmation
	// target in blocks used by estimatesmartfee to get the optimal fee for a
	// redeem transaction.
	defaultRedeemConfTarget = 1
)

var configOpts = []*asset.ConfigOption{
	{
		Key:         "account",
		DisplayName: "Account Name",
		Description: "dcrwallet account name",
	},
	{
		Key:         "fallbackfee",
		DisplayName: "Fallback fee rate",
		Description: "The fee rate to use for fee payment and withdrawals when " +
			"estimatesmartfee is not available. Units: DCR/kB",
		DefaultValue: defaultFee * 1000 / 1e8,
	},
	{
		Key:         "feeratelimit",
		DisplayName: "Highest acceptable fee rate",
		Description: "This is the highest network fee rate you are willing to " +
			"pay on swap transactions. If feeratelimit is lower than a market's " +
			"maxfeerate, you will not be able to trade on that market with this " +
			"wallet.  Units: DCR/kB",
		DefaultValue: defaultFeeRateLimit * 1000 / 1e8,
	},
	{
		Key:         "redeemconftarget",
		DisplayName: "Redeem confirmation target",
		Description: "The target number of blocks for the redeem transaction " +
			"to get a confirmation. Used to set the transaction's fee rate." +
			" (default: 1 block)",
		DefaultValue: defaultRedeemConfTarget,
	},
	{
		Key:         "txsplit",
		DisplayName: "Pre-size funding inputs",
		Description: "When placing an order, create a \"split\" transaction to " +
			"fund the order without locking more of the wallet balance than " +
			"necessary. Otherwise, excess funds may be reserved to fund the order " +
			"until the first swap contract is broadcast during match settlement, or " +
			"the order is canceled. This an extra transaction for which network " +
			"mining fees are paid.  Used only for standing-type orders, e.g. " +
			"limit orders without immediate time-in-force.",
		IsBoolean: true,
	},
}

// Driver implements decred.org/dcrdex/client/asset.Driver.
type Driver struct {
	wallet     *wallet.Wallet
	walletDesc string
}

// RegisterDriver registers the driver for the dcr asset backend. Requires a
// wallet instance to provide wallet functionality to the DEX.
func RegisterDriver(wallet *wallet.Wallet, walletDesc string) {
	asset.Register(dcr.BipID, &Driver{
		wallet:     wallet,
		walletDesc: walletDesc,
	})
}

// Setup creates the DCR exchange wallet.
func (d *Driver) Setup(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return dcr.NewWallet(cfg, logger, network, NewWallet(d.wallet, d.walletDesc))
}

// DecodeCoinID creates a human-readable representation of a coin ID for Decred.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	txid, vout, err := decodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v:%d", txid, vout), err
}

// Info returns basic information about the decred wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	wInfo := dcr.WalletInfo
	wInfo.DefaultConfigPath = ""
	wInfo.ConfigOpts = configOpts
	return wInfo
}

// decodeCoinID decodes the coin ID into a tx hash and a vout.
func decodeCoinID(coinID dex.Bytes) (*chainhash.Hash, uint32, error) {
	if len(coinID) != 36 {
		return nil, 0, fmt.Errorf("coin ID wrong length. expected 36, got %d", len(coinID))
	}
	var txHash chainhash.Hash
	copy(txHash[:], coinID[:32])
	return &txHash, binary.BigEndian.Uint32(coinID[32:]), nil
}
