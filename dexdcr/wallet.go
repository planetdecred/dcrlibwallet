package dexdcr

import (
	"context"
	"fmt"
	"math"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/rpc/client/dcrwallet"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v2/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

// SpvWallet is a decred wallet backend for the DEX. The backend is how the DEX
// client app communicates with the Decred blockchain and wallet.
// Satisfies the decred.org/dcrdex/client/asset/dcr.SpvWallet interface.
type SpvWallet struct {
	*wallet.Wallet
	desc string // a human-readable description of this wallet, for logging purposes.

	initialized bool
	connected   bool

	chainParams *chaincfg.Params
	log         dex.Logger
}

func NewWallet(w *wallet.Wallet, desc string) *SpvWallet {
	return &SpvWallet{
		Wallet: w,
		desc:   desc,
	}
}

// Ensure that Wallet satisfies the decred.org/dcrdex/client/asset/dcr.Wallet
// interface.
var _ dcr.Wallet = (*SpvWallet)(nil)

var notImplemented = fmt.Errorf("not yet implemented")

// Initialize prepares the wallet for use.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) Initialize(cfg *asset.WalletConfig, dcrCfg *dcr.Config, chainParams *chaincfg.Params, logger dex.Logger) error {
	if w.initialized {
		return fmt.Errorf("wallet already initialized")
	}
	if w.Wallet == nil {
		return fmt.Errorf("wallet is not properly set up")
	}
	if w.Wallet.ChainParams().Net != chainParams.Net {
		return fmt.Errorf("cannot initialize %s wallet with %s params", w.Wallet.ChainParams().Name, chainParams.Name)
	}

	// Ensure the wallet is connected to an spv backend.
	if _, err := w.spvSyncer(); err != nil {
		return err
	}

	w.chainParams = chainParams
	w.log = logger
	w.initialized = true
	return nil
}

// Connect establishes a connection to the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) Connect(ctx context.Context) error {
	if !w.initialized {
		return fmt.Errorf("wallet is not initialized")
	}
	if w.connected {
		return fmt.Errorf("wallet already connected")
	}

	w.connected = true
	w.log.Infof("Connected to wallet %s", w.desc)
	return nil
}

// Disconnect shuts down access to the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) Disconnect() {
	if w.connected {
		w.connected = false
		w.log.Infof("Disconnected wallet %s", w.desc)
	}
}

// Disconnected returns true if the wallet is not connected.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) Disconnected() bool {
	return !w.connected
}

// SyncStatus returns the wallet's sync status.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) SyncStatus(ctx context.Context) (bool, float32, error) {
	syncer, err := w.spvSyncer()
	if err != nil {
		return false, 0, err
	}

	walletBestHash, walletBestHeight := w.Wallet.MainChainTip(ctx)
	bestBlock, err := w.Wallet.BlockInfo(ctx, wallet.NewBlockIdentifierFromHash(&walletBestHash))
	if err != nil {
		return false, 0, err
	}
	_24HoursAgo := time.Now().UTC().Add(-24 * time.Hour).Unix()
	isInitialBlockDownload := bestBlock.Timestamp < _24HoursAgo // assume IBD if the wallet's best block is older than 24 hours ago

	targetHeight := syncer.EstimateMainChainTip()
	var headersFetchProgress float32
	blocksToFetch := targetHeight - walletBestHeight
	if blocksToFetch <= 0 {
		headersFetchProgress = 1
	} else {
		totalHeadersToFetch := targetHeight - w.Wallet.InitialHeight()
		headersFetchProgress = 1 - (float32(blocksToFetch) / float32(totalHeadersToFetch))
	}

	syncedAndReadyForUse := syncer.Synced() && !isInitialBlockDownload
	return syncedAndReadyForUse, headersFetchProgress, nil
}

// AccountOwnsAddress checks if the provided address belongs to the
// specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) AccountOwnsAddress(ctx context.Context, account, address string) (bool, error) {
	addr, err := stdaddr.DecodeAddress(address, w.chainParams)
	if err != nil {
		return false, err
	}
	a, err := w.Wallet.KnownAddress(ctx, addr)
	if err != nil {
		return false, err
	}
	return a.AccountName() == account, nil
}

// AccountBalance returns the balance breakdown for the specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) AccountBalance(ctx context.Context, account string, confirms int32) (*walletjson.GetAccountBalanceResult, error) {
	acctNumber, err := w.accountNumber(ctx, account)
	if err != nil {
		return nil, err
	}

	balance, err := w.Wallet.AccountBalance(ctx, acctNumber, confirms)
	if err != nil {
		return nil, err
	}

	return &walletjson.GetAccountBalanceResult{
		AccountName:             account,
		ImmatureCoinbaseRewards: balance.ImmatureCoinbaseRewards.ToCoin(),
		ImmatureStakeGeneration: balance.ImmatureStakeGeneration.ToCoin(),
		LockedByTickets:         balance.LockedByTickets.ToCoin(),
		Spendable:               balance.Spendable.ToCoin(),
		Total:                   balance.Total.ToCoin(),
		Unconfirmed:             balance.Unconfirmed.ToCoin(),
		VotingAuthority:         balance.VotingAuthority.ToCoin(),
	}, nil
}

// LockedOutputs fetches locked outputs for the specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) LockedOutputs(ctx context.Context, account string) ([]chainjson.TransactionInput, error) {
	return w.Wallet.LockedOutpoints(ctx, account)
}

// EstimateSmartFeeRate returns a smart feerate estimate.
// NOTE: SPV wallets are unable to estimate feerates, so this will always
// return 0.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) EstimateSmartFeeRate(ctx context.Context, confTarget int64, mode chainjson.EstimateSmartFeeMode) (float64, error) {
	return 0, nil
}

// Unspents fetches unspent outputs for the specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) Unspents(ctx context.Context, account string) ([]walletjson.ListUnspentResult, error) {
	// the listunspent rpc handler uses 9999999 as default for maxconf
	unspents, err := w.Wallet.ListUnspent(ctx, 0, math.MaxInt32, nil, account)
	if err != nil {
		return nil, err
	}
	result := make([]walletjson.ListUnspentResult, len(unspents))
	for i, unspent := range unspents {
		result[i] = *unspent
	}
	return result, nil
}

// GetChangeAddress returns a change address from the specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetChangeAddress(ctx context.Context, account string) (stdaddr.Address, error) {
	acctNumber, err := w.accountNumber(ctx, account)
	if err != nil {
		return nil, err
	}
	return w.Wallet.NewChangeAddress(ctx, acctNumber)
}

// LockUnspent locks or unlocks the specified outpoint.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error {
	if unlock && len(ops) == 0 {
		w.Wallet.ResetLockedOutpoints()
		return nil
	}

	for _, op := range ops {
		if unlock {
			w.Wallet.UnlockOutpoint(&op.Hash, op.Index)
		} else {
			w.Wallet.LockOutpoint(&op.Hash, op.Index)
		}
	}
	return nil
}

// GetTxOut returns information about an unspent tx output.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8, mempool bool) (*chainjson.GetTxOutResult, error) {
	return nil, notImplemented
}

// GetNewAddressGapPolicy returns an address from the specified account using
// the specified gap policy.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetNewAddressGapPolicy(ctx context.Context, account string, gap dcrwallet.GapPolicy) (stdaddr.Address, error) {
	acctNumber, err := w.accountNumber(ctx, account)
	if err != nil {
		return nil, err
	}

	var policy wallet.NextAddressCallOption
	switch gap {
	case dcrwallet.GapPolicyWrap:
		policy = wallet.WithGapPolicyWrap()
	case dcrwallet.GapPolicyIgnore:
		policy = wallet.WithGapPolicyIgnore()
	case dcrwallet.GapPolicyError:
		policy = wallet.WithGapPolicyError()
	default:
		return nil, fmt.Errorf("unknown gap policy %q", gap)
	}

	return w.Wallet.NewExternalAddress(ctx, acctNumber, policy)
}

// SignRawTransaction signs the provided transaction.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) SignRawTransaction(ctx context.Context, tx *wire.MsgTx) (*walletjson.SignRawTransactionResult, error) {
	return nil, notImplemented
}

// SendRawTransaction broadcasts the provided transaction to the Decred
// network.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	return nil, notImplemented
}

// GetBlockHeaderVerbose returns block header info for the specified block hash.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetBlockHeaderVerbose(ctx context.Context, blockHash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error) {
	return nil, notImplemented
}

// GetBlockVerbose returns information about a block, optionally including verbose
// tx info.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetBlockVerbose(ctx context.Context, blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error) {
	return nil, notImplemented
}

// GetTransaction returns the details of a wallet tx, if the wallet contains a
// tx with the provided hash. Returns asset.CoinNotFoundError if the tx is not
// found in the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error) {
	return nil, notImplemented
}

// GetRawTransactionVerbose returns details of the tx with the provided hash.
// NOTE: SPV wallets are unable to look up non-wallet transactions so this will
// always return a not-supported error.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetRawTransactionVerbose(ctx context.Context, txHash *chainhash.Hash) (*chainjson.TxRawResult, error) {
	return nil, notImplemented
}

// GetRawMempool returns hashes for all txs of the specified type in the node's
// mempool.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error) {
	return nil, notImplemented
}

// GetBestBlock returns the hash and height of the wallet's best block.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error) {
	walletBestHash, walletBestHeight := w.Wallet.MainChainTip(ctx)
	return &walletBestHash, int64(walletBestHeight), nil
}

// GetBlockHash returns the hash of the mainchain block at the specified height.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
	return nil, notImplemented
}

// BlockCFilter fetches the block filter info for the specified block.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) BlockCFilter(ctx context.Context, blockHash string) (filter, key string, err error) {
	return "", "", notImplemented
}

// LockWallet locks the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) LockWallet(_ context.Context) error {
	// TODO: dcrlibwallet considers accountmixer status before locking the wallet
	w.Wallet.Lock()
	return nil
}

// UnlockWallet unlocks the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) UnlockWallet(ctx context.Context, passphrase string, timeoutSecs int64) error {
	timeout := time.Second * time.Duration(timeoutSecs)
	lockAfter := time.After(timeout)
	return w.Wallet.Unlock(ctx, []byte(passphrase), lockAfter)
}

// WalletUnlocked returns true if the wallet is unlocked.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) WalletUnlocked(_ context.Context) bool {
	return !w.Wallet.Locked()
}

// AccountUnlocked returns true if the specified account is unlocked.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) AccountUnlocked(ctx context.Context, account string) (*walletjson.AccountUnlockedResult, error) {
	acctNumber, err := w.accountNumber(ctx, account)
	if err != nil {
		return nil, err
	}

	encrypted, err := w.Wallet.AccountHasPassphrase(ctx, acctNumber)
	if err != nil {
		return nil, err
	}
	if !encrypted {
		return &walletjson.AccountUnlockedResult{}, nil
	}

	unlocked, err := w.Wallet.AccountUnlocked(ctx, acctNumber)
	if err != nil {
		return nil, err
	}

	return &walletjson.AccountUnlockedResult{
		Encrypted: true,
		Unlocked:  &unlocked,
	}, nil
}

// LockAccount locks the specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) LockAccount(ctx context.Context, account string) error {
	acctNumber, err := w.accountNumber(ctx, account)
	if err != nil {
		return err
	}
	return w.Wallet.LockAccount(ctx, acctNumber)
}

// UnlockAccount unlocks the specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) UnlockAccount(ctx context.Context, account, passphrase string) error {
	return notImplemented
}

// AddressPrivKey fetches the privkey for the specified address.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (w *SpvWallet) AddressPrivKey(ctx context.Context, address stdaddr.Address) (*dcrutil.WIF, error) {
	return nil, notImplemented
}

func (w *SpvWallet) accountNumber(ctx context.Context, account string) (uint32, error) {
	acctNumber, err := w.Wallet.AccountNumber(ctx, account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return 0, fmt.Errorf("%q account does not exist", account)
		}
		return 0, err
	}
	return acctNumber, nil
}
