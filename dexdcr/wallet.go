// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dexdcr

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/rpc/client/dcrwallet"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v2/wallet"
	"decred.org/dcrwallet/v2/wallet/txrules"
	"decred.org/dcrwallet/v2/wallet/udb"
	"github.com/decred/dcrd/blockchain/stake/v4"
	blockchain "github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/decred/slog"
)

const (
	// sstxCommitmentString is the string to insert when a verbose
	// transaction output's pkscript type is a ticket commitment.
	sstxCommitmentString = "sstxcommitment"
)

// SpvWallet is a decred wallet backend for the DEX. The backend is how the DEX
// client app communicates with the Decred blockchain and wallet.
// Satisfies the decred.org/dcrdex/client/asset/dcr.Wallet interface.
type SpvWallet struct {
	wallet *wallet.Wallet
	desc   string // a human-readable description of this wallet, for logging purposes.

	chainParams *chaincfg.Params
	log         dex.Logger

	connected uint32 // atomic
}

// Ensure that SpvWallet satisfies the decred.org/dcrdex/client/asset/dcr.Wallet
// interface.
var _ dcr.Wallet = (*SpvWallet)(nil)

// Connect establishes a connection to the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) Connect(ctx context.Context) error {
	if !atomic.CompareAndSwapUint32(&spvw.connected, 0, 1) {
		return fmt.Errorf("already connected")
	}

	var connectSuccess bool
	defer func() {
		if !connectSuccess {
			atomic.StoreUint32(&spvw.connected, 0)
		}
	}()

	if spvw.wallet == nil {
		return fmt.Errorf("this SpvWallet is not properly set up, did you use SpvWalletConstructor()?")
	}
	if _, err := spvw.spvSyncer(); err != nil { // ensure the wallet is connected to the Decred network via an SPV syncer.
		return err
	}

	connectSuccess = true
	spvw.log.Infof("Connected to wallet %s", spvw.desc)
	return nil
}

// Disconnect shuts down access to the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) Disconnect() {
	if atomic.CompareAndSwapUint32(&spvw.connected, 1, 0) {
		spvw.log.Infof("Disconnected wallet %s", spvw.desc)
	}
}

// Disconnected returns true if the wallet is not connected.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) Disconnected() bool {
	return atomic.LoadUint32(&spvw.connected) == 0
}

// Network returns the network of the connected wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) Network(ctx context.Context) (wire.CurrencyNet, error) {
	return spvw.wallet.ChainParams().Net, nil
}

// SpvMode returns through if the wallet is connected to the Decred
// network via SPV peers.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) SpvMode() bool {
	return true
}

// NotifyOnTipChange registers a callback function that the should be
// invoked when the wallet sees new mainchain blocks. The return value
// indicates if this notification can be provided.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) NotifyOnTipChange(ctx context.Context, cb dcr.TipChangeCallback) bool {
	// TODO: Implement a tip change notification handler to prevent bestblock polling.
	return false
}

// SyncStatus returns the wallet's sync status.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) SyncStatus(ctx context.Context) (bool, float32, error) {
	syncer, err := spvw.spvSyncer()
	if err != nil {
		return false, 0, err
	}

	walletBestHash, walletBestHeight := spvw.wallet.MainChainTip(ctx)
	bestBlock, err := spvw.wallet.BlockInfo(ctx, wallet.NewBlockIdentifierFromHash(&walletBestHash))
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
		totalHeadersToFetch := targetHeight - spvw.wallet.InitialHeight()
		headersFetchProgress = 1 - (float32(blocksToFetch) / float32(totalHeadersToFetch))
	}

	syncedAndReadyForUse := syncer.Synced() && !isInitialBlockDownload
	return syncedAndReadyForUse, headersFetchProgress, nil
}

// AccountOwnsAddress checks if the provided address belongs to the
// specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) AccountOwnsAddress(ctx context.Context, account, address string) (bool, error) {
	addr, err := stdaddr.DecodeAddress(address, spvw.chainParams)
	if err != nil {
		return false, err
	}
	a, err := spvw.wallet.KnownAddress(ctx, addr)
	if err != nil {
		return false, err
	}
	return a.AccountName() == account, nil
}

// AccountBalance returns the balance breakdown for the specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) AccountBalance(ctx context.Context, account string, confirms int32) (*walletjson.GetAccountBalanceResult, error) {
	acctNumber, err := spvw.accountNumber(ctx, account)
	if err != nil {
		return nil, err
	}

	balance, err := spvw.wallet.AccountBalance(ctx, acctNumber, confirms)
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
func (spvw *SpvWallet) LockedOutputs(ctx context.Context, account string) ([]chainjson.TransactionInput, error) {
	return spvw.wallet.LockedOutpoints(ctx, account)
}

// EstimateSmartFeeRate returns a smart feerate estimate.
// NOTE: SPV wallets are unable to estimate feerates, so this will always
// return 0.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) EstimateSmartFeeRate(ctx context.Context, confTarget int64, mode chainjson.EstimateSmartFeeMode) (float64, error) {
	return 0, nil
}

// Unspents fetches unspent outputs for the specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) Unspents(ctx context.Context, account string) ([]walletjson.ListUnspentResult, error) {
	// the listunspent rpc handler uses 9999999 as default for maxconf
	unspents, err := spvw.wallet.ListUnspent(ctx, 0, math.MaxInt32, nil, account)
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
func (spvw *SpvWallet) GetChangeAddress(ctx context.Context, account string) (stdaddr.Address, error) {
	acctNumber, err := spvw.accountNumber(ctx, account)
	if err != nil {
		return nil, err
	}
	return spvw.wallet.NewChangeAddress(ctx, acctNumber)
}

// LockUnspent locks or unlocks the specified outpoint.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error {
	if unlock && len(ops) == 0 {
		spvw.wallet.ResetLockedOutpoints()
		return nil
	}

	for _, op := range ops {
		if unlock {
			spvw.wallet.UnlockOutpoint(&op.Hash, op.Index)
		} else {
			spvw.wallet.LockOutpoint(&op.Hash, op.Index)
		}
	}
	return nil
}

// TxConfs returns the number of confirmations for the provided tx, if it
// is known to the wallet. Returns asset.CoinNotFoundError for txs that are
// not tracked by the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) TxConfs(ctx context.Context, txHash *chainhash.Hash) (uint32, error) {
	tx, err := spvw.GetTransaction(ctx, txHash)
	if err != nil {
		return 0, err
	}
	return uint32(tx.Confirmations), nil
}

// GetTxOut returns information about an unspent tx output, if found and
// is unspent. Use wire.TxTreeUnknown if the output tree is unknown, the
// correct tree will be returned if the unspent output is found.
// An asset.CoinNotFoundError is returned if the unspent output cannot be
// located. UnspentOutput is only guaranteed to return results for outputs
// that pay to the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8, mempool bool) (*chainjson.GetTxOutResult, error) {
	// gettxout in spv mode returns details for outputs as long
	// they exist, pay to the wallet and are unspent.
	var checkTrees []int8
	switch {
	case tree == wire.TxTreeUnknown:
		checkTrees = []int8{wire.TxTreeRegular, wire.TxTreeStake}
	case tree == wire.TxTreeRegular || tree == wire.TxTreeStake:
		checkTrees = []int8{tree}
	default:
		return nil, fmt.Errorf("tx tree must be regular or stake")
	}

	// Attempt to read the unspent txout info from wallet.
	var utxo *udb.Credit
	var err error
	for _, tree := range checkTrees {
		outpoint := wire.OutPoint{Hash: *txHash, Index: index, Tree: tree}
		utxo, err = spvw.wallet.UnspentOutput(ctx, outpoint, mempool)
		if err != nil {
			if errors.Is(err, errors.NotExist) {
				continue
			}
			return nil, err
		}
	}
	if utxo == nil {
		return nil, asset.CoinNotFoundError
	}

	// Disassemble script into single line printable format.  The
	// disassembled string will contain [error] inline if the script
	// doesn't fully parse, so ignore the error here.
	disbuf, _ := txscript.DisasmString(utxo.PkScript)

	// Get further info about the script.  Ignore the error here since an
	// error means the script couldn't parse and there is no additional
	// information about it anyways.
	scriptClass, addrs, reqSigs, _ := txscript.ExtractPkScriptAddrs(
		0, utxo.PkScript, spvw.chainParams, true) // Yes treasury
	addresses := make([]string, len(addrs))
	for i, addr := range addrs {
		addresses[i] = addr.String()
	}

	bestHash, bestHeight := spvw.wallet.MainChainTip(ctx)
	var confirmations int64
	if utxo.Block.Height != -1 {
		confirmations = int64(confirms(utxo.Block.Height, bestHeight))
	}

	return &chainjson.GetTxOutResult{
		BestBlock:     bestHash.String(),
		Confirmations: confirmations,
		Value:         utxo.Amount.ToCoin(),
		ScriptPubKey: chainjson.ScriptPubKeyResult{
			Asm:       disbuf,
			Hex:       hex.EncodeToString(utxo.PkScript),
			ReqSigs:   int32(reqSigs),
			Type:      scriptClass.String(),
			Addresses: addresses,
		},
		Coinbase: utxo.FromCoinBase,
	}, nil
}

// GetNewAddressGapPolicy returns an address from the specified account using
// the specified gap policy.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) GetNewAddressGapPolicy(ctx context.Context, account string, gap dcrwallet.GapPolicy) (stdaddr.Address, error) {
	acctNumber, err := spvw.accountNumber(ctx, account)
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

	return spvw.wallet.NewExternalAddress(ctx, acctNumber, policy)
}

// SignRawTransaction signs the provided transaction.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) SignRawTransaction(ctx context.Context, txHex string) (*walletjson.SignRawTransactionResult, error) {
	tx := wire.NewMsgTx()
	if err := tx.Deserialize(hex.NewDecoder(strings.NewReader(txHex))); err != nil {
		return nil, err
	}

	if len(tx.TxIn) == 0 {
		return nil, fmt.Errorf("transaction with no inputs cannot be signed")
	}

	signErrs, err := spvw.wallet.SignTransaction(ctx, tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.Grow(2 * tx.SerializeSize())
	err = tx.Serialize(hex.NewEncoder(&b))
	if err != nil {
		return nil, err
	}

	signErrors := make([]walletjson.SignRawTransactionError, 0, len(signErrs))
	for _, e := range signErrs {
		input := tx.TxIn[e.InputIndex]
		signErrors = append(signErrors, walletjson.SignRawTransactionError{
			TxID:      input.PreviousOutPoint.Hash.String(),
			Vout:      input.PreviousOutPoint.Index,
			ScriptSig: hex.EncodeToString(input.SignatureScript),
			Sequence:  input.Sequence,
			Error:     e.Error.Error(),
		})
	}

	return &walletjson.SignRawTransactionResult{
		Hex:      b.String(),
		Complete: len(signErrors) == 0,
		Errors:   signErrors,
	}, nil
}

// SendRawTransaction broadcasts the provided transaction to the Decred
// network.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	n, err := spvw.wallet.NetworkBackend()
	if err != nil {
		return nil, err
	}

	if !allowHighFees {
		highFees, err := txrules.TxPaysHighFees(tx)
		if err != nil {
			return nil, err
		}
		if highFees {
			return nil, fmt.Errorf("high fees")
		}
	}

	return spvw.wallet.PublishTransaction(ctx, tx, n)
}

// GetBlockHeaderVerbose returns block header info for the specified block hash.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) GetBlockHeaderVerbose(ctx context.Context, blockHash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error) {
	blockHeader, err := spvw.wallet.BlockHeader(ctx, blockHash)
	if err != nil {
		return nil, err
	}

	// Get next block hash unless there are none.
	var nextHashString string
	confirmations := int64(-1)
	mainChainHasBlock, _, err := spvw.wallet.BlockInMainChain(ctx, blockHash)
	if err != nil {
		return nil, fmt.Errorf("error checking if block is in mainchain: %v", err)
	}
	if mainChainHasBlock {
		blockHeight := int32(blockHeader.Height)
		_, bestHeight := spvw.wallet.MainChainTip(ctx)
		if blockHeight < bestHeight {
			nextBlockID := wallet.NewBlockIdentifierFromHeight(blockHeight + 1)
			nextBlockInfo, err := spvw.wallet.BlockInfo(ctx, nextBlockID)
			if err != nil {
				return nil, fmt.Errorf("info not found for next block: %v", err)
			}
			nextHashString = nextBlockInfo.Hash.String()
		}
		confirmations = int64(confirms(blockHeight, bestHeight))
	}

	// Calculate past median time. Look at the last 11 blocks, starting
	// with the requested block, which is consistent with dcrd.
	timestamps := make([]int64, 0, 11)
	for i := 0; i < cap(timestamps); i++ {
		timestamps = append(timestamps, blockHeader.Timestamp.Unix())
		if blockHeader.Height == 0 {
			break
		}
		blockHeader, err = spvw.wallet.BlockHeader(ctx, &blockHeader.PrevBlock)
		if err != nil {
			return nil, fmt.Errorf("unable to calculate median block time: %w", err)
		}
	}
	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})
	medianTimestamp := timestamps[len(timestamps)/2]
	medianTime := time.Unix(medianTimestamp, 0)

	return &chainjson.GetBlockHeaderVerboseResult{
		Hash:          blockHash.String(),
		Confirmations: confirmations,
		Version:       blockHeader.Version,
		MerkleRoot:    blockHeader.MerkleRoot.String(),
		StakeRoot:     blockHeader.StakeRoot.String(),
		VoteBits:      blockHeader.VoteBits,
		FinalState:    hex.EncodeToString(blockHeader.FinalState[:]),
		Voters:        blockHeader.Voters,
		FreshStake:    blockHeader.FreshStake,
		Revocations:   blockHeader.Revocations,
		PoolSize:      blockHeader.PoolSize,
		Bits:          strconv.FormatInt(int64(blockHeader.Bits), 16),
		SBits:         dcrutil.Amount(blockHeader.SBits).ToCoin(),
		Height:        blockHeader.Height,
		Size:          blockHeader.Size,
		Time:          blockHeader.Timestamp.Unix(),
		MedianTime:    medianTime.Unix(),
		Nonce:         blockHeader.Nonce,
		ExtraData:     hex.EncodeToString(blockHeader.ExtraData[:]),
		StakeVersion:  blockHeader.StakeVersion,
		Difficulty:    difficultyRatio(blockHeader.Bits, spvw.chainParams, spvw.log),
		ChainWork:     fmt.Sprintf("%064x", blockchain.CalcWork(blockHeader.Bits)),
		PreviousHash:  blockHeader.PrevBlock.String(),
		NextHash:      nextHashString,
	}, nil
}

// GetBlockVerbose returns information about a block, optionally including verbose
// tx info.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) GetBlockVerbose(ctx context.Context, blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error) {
	n, err := spvw.wallet.NetworkBackend()
	if err != nil {
		return nil, err
	}

	blocks, err := n.Blocks(ctx, []*chainhash.Hash{blockHash})
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		// Should never happen but protects against a possible panic on
		// the following code.
		return nil, fmt.Errorf("network returned 0 blocks")
	}

	blk := blocks[0]

	// Get next block hash unless there are none.
	var nextHashString string
	blockHeader := &blk.Header
	confirmations := int64(-1)
	mainChainHasBlock, _, err := spvw.wallet.BlockInMainChain(ctx, blockHash)
	if err != nil {
		return nil, fmt.Errorf("error checking if block is in mainchain: %v", err)
	}
	if mainChainHasBlock {
		blockHeight := int32(blockHeader.Height)
		_, bestHeight := spvw.wallet.MainChainTip(ctx)
		if blockHeight < bestHeight {
			nextBlockID := wallet.NewBlockIdentifierFromHeight(blockHeight + 1)
			nextBlockInfo, err := spvw.wallet.BlockInfo(ctx, nextBlockID)
			if err != nil {
				return nil, fmt.Errorf("info not found for next block: %v", err)
			}
			nextHashString = nextBlockInfo.Hash.String()
		}
		confirmations = int64(confirms(blockHeight, bestHeight))
	}

	sbitsFloat := float64(blockHeader.SBits) / dcrutil.AtomsPerCoin
	blockReply := &chainjson.GetBlockVerboseResult{
		Hash:          blockHash.String(),
		Version:       blockHeader.Version,
		MerkleRoot:    blockHeader.MerkleRoot.String(),
		StakeRoot:     blockHeader.StakeRoot.String(),
		PreviousHash:  blockHeader.PrevBlock.String(),
		Nonce:         blockHeader.Nonce,
		VoteBits:      blockHeader.VoteBits,
		FinalState:    hex.EncodeToString(blockHeader.FinalState[:]),
		Voters:        blockHeader.Voters,
		FreshStake:    blockHeader.FreshStake,
		Revocations:   blockHeader.Revocations,
		PoolSize:      blockHeader.PoolSize,
		Time:          blockHeader.Timestamp.Unix(),
		StakeVersion:  blockHeader.StakeVersion,
		Confirmations: confirmations,
		Height:        int64(blockHeader.Height),
		Size:          int32(blk.Header.Size),
		Bits:          strconv.FormatInt(int64(blockHeader.Bits), 16),
		SBits:         sbitsFloat,
		Difficulty:    difficultyRatio(blockHeader.Bits, spvw.chainParams, spvw.log),
		ChainWork:     fmt.Sprintf("%064x", blockchain.CalcWork(blockHeader.Bits)),
		ExtraData:     hex.EncodeToString(blockHeader.ExtraData[:]),
		NextHash:      nextHashString,
	}

	// Determine if the treasury rules are active for the block.
	// All txs in the stake tree must be version 3 once the treasury agenda
	// is active.
	isTreasuryEnabled := blk.STransactions[0].Version == wire.TxVersionTreasury

	if !verboseTx {
		transactions := blk.Transactions
		txNames := make([]string, len(transactions))
		for i, tx := range transactions {
			txNames[i] = tx.TxHash().String()
		}
		blockReply.Tx = txNames

		stransactions := blk.STransactions
		stxNames := make([]string, len(stransactions))
		for i, tx := range stransactions {
			stxNames[i] = tx.TxHash().String()
		}
		blockReply.STx = stxNames
	} else {
		txns := blk.Transactions
		rawTxns := make([]chainjson.TxRawResult, len(txns))
		for i, tx := range txns {
			rawTxn, err := createTxRawResult(spvw.chainParams, tx, uint32(i), blockHeader, confirmations, isTreasuryEnabled, spvw.log)
			if err != nil {
				return nil, fmt.Errorf("could not create transaction: %v", err)
			}
			rawTxns[i] = *rawTxn
		}
		blockReply.RawTx = rawTxns

		stxns := blk.STransactions
		rawSTxns := make([]chainjson.TxRawResult, len(stxns))
		for i, tx := range stxns {
			rawSTxn, err := createTxRawResult(spvw.chainParams, tx, uint32(i), blockHeader, confirmations, isTreasuryEnabled, spvw.log)
			if err != nil {
				return nil, fmt.Errorf("could not create stake transaction: %v", err)
			}
			rawSTxns[i] = *rawSTxn
		}
		blockReply.RawSTx = rawSTxns
	}

	return blockReply, nil
}

func createTxRawResult(chainParams *chaincfg.Params, mtx *wire.MsgTx, blkIdx uint32, blkHeader *wire.BlockHeader,
	confirmations int64, isTreasuryEnabled bool, log slog.Logger) (*chainjson.TxRawResult, error) {

	b := new(strings.Builder)
	b.Grow(2 * mtx.SerializeSize())
	err := mtx.Serialize(hex.NewEncoder(b))
	if err != nil {
		return nil, err
	}

	txReply := &chainjson.TxRawResult{
		Hex:        b.String(),
		Txid:       mtx.CachedTxHash().String(),
		Vin:        createVinList(mtx, isTreasuryEnabled),
		Vout:       createVoutList(mtx, chainParams, nil, isTreasuryEnabled, log),
		Version:    int32(mtx.Version),
		LockTime:   mtx.LockTime,
		Expiry:     mtx.Expiry,
		BlockIndex: blkIdx,
	}

	if blkHeader != nil {
		// This is not a typo, they are identical in bitcoind as well.
		txReply.BlockHeight = int64(blkHeader.Height)
		txReply.Time = blkHeader.Timestamp.Unix()
		txReply.Blocktime = blkHeader.Timestamp.Unix()
		txReply.BlockHash = blkHeader.BlockHash().String()
		txReply.Confirmations = confirmations
	}

	return txReply, nil
}

// createVinList returns a slice of JSON objects for the inputs of the passed
// transaction.
func createVinList(mtx *wire.MsgTx, isTreasuryEnabled bool) []chainjson.Vin {
	// Treasurybase transactions only have a single txin by definition.
	//
	// NOTE: This check MUST come before the coinbase check because a
	// treasurybase will be identified as a coinbase as well.
	vinList := make([]chainjson.Vin, len(mtx.TxIn))
	if isTreasuryEnabled && blockchain.IsTreasuryBase(mtx) {
		txIn := mtx.TxIn[0]
		vinEntry := &vinList[0]
		vinEntry.Treasurybase = true
		vinEntry.Sequence = txIn.Sequence
		vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
		vinEntry.BlockHeight = txIn.BlockHeight
		vinEntry.BlockIndex = txIn.BlockIndex
		return vinList
	}

	// Coinbase transactions only have a single txin by definition.
	if blockchain.IsCoinBaseTx(mtx, isTreasuryEnabled) {
		txIn := mtx.TxIn[0]
		vinEntry := &vinList[0]
		vinEntry.Coinbase = hex.EncodeToString(txIn.SignatureScript)
		vinEntry.Sequence = txIn.Sequence
		vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
		vinEntry.BlockHeight = txIn.BlockHeight
		vinEntry.BlockIndex = txIn.BlockIndex
		return vinList
	}

	// Treasury spend transactions only have a single txin by definition.
	if isTreasuryEnabled && stake.IsTSpend(mtx) {
		txIn := mtx.TxIn[0]
		vinEntry := &vinList[0]
		vinEntry.TreasurySpend = hex.EncodeToString(txIn.SignatureScript)
		vinEntry.Sequence = txIn.Sequence
		vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
		vinEntry.BlockHeight = txIn.BlockHeight
		vinEntry.BlockIndex = txIn.BlockIndex
		return vinList
	}

	// Stakebase transactions (votes) have two inputs: a null stake base
	// followed by an input consuming a ticket's stakesubmission.
	isSSGen := stake.IsSSGen(mtx, isTreasuryEnabled)

	for i, txIn := range mtx.TxIn {
		// Handle only the null input of a stakebase differently.
		if isSSGen && i == 0 {
			vinEntry := &vinList[0]
			vinEntry.Stakebase = hex.EncodeToString(txIn.SignatureScript)
			vinEntry.Sequence = txIn.Sequence
			vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
			vinEntry.BlockHeight = txIn.BlockHeight
			vinEntry.BlockIndex = txIn.BlockIndex
			continue
		}

		// The disassembled string will contain [error] inline
		// if the script doesn't fully parse, so ignore the
		// error here.
		disbuf, _ := txscript.DisasmString(txIn.SignatureScript)

		vinEntry := &vinList[i]
		vinEntry.Txid = txIn.PreviousOutPoint.Hash.String()
		vinEntry.Vout = txIn.PreviousOutPoint.Index
		vinEntry.Tree = txIn.PreviousOutPoint.Tree
		vinEntry.Sequence = txIn.Sequence
		vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
		vinEntry.BlockHeight = txIn.BlockHeight
		vinEntry.BlockIndex = txIn.BlockIndex
		vinEntry.ScriptSig = &chainjson.ScriptSig{
			Asm: disbuf,
			Hex: hex.EncodeToString(txIn.SignatureScript),
		}
	}

	return vinList
}

// createVoutList returns a slice of JSON objects for the outputs of the passed
// transaction.
func createVoutList(mtx *wire.MsgTx, chainParams *chaincfg.Params, filterAddrMap map[string]struct{}, isTreasuryEnabled bool, log slog.Logger) []chainjson.Vout {
	txType := stake.DetermineTxType(mtx, isTreasuryEnabled, false)
	voutList := make([]chainjson.Vout, 0, len(mtx.TxOut))
	for i, v := range mtx.TxOut {
		// The disassembled string will contain [error] inline if the
		// script doesn't fully parse, so ignore the error here.
		disbuf, _ := txscript.DisasmString(v.PkScript)

		// Attempt to extract addresses from the public key script.  In
		// the case of stake submission transactions, the odd outputs
		// contain a commitment address, so detect that case
		// accordingly.
		var addrs []stdaddr.Address
		var scriptClass string
		var reqSigs int
		var commitAmt *dcrutil.Amount
		if txType == stake.TxTypeSStx && (i%2 != 0) {
			scriptClass = sstxCommitmentString
			addr, err := stake.AddrFromSStxPkScrCommitment(v.PkScript,
				chainParams)
			if err != nil {
				log.Warnf("failed to decode ticket "+
					"commitment addr output for tx hash "+
					"%v, output idx %v", mtx.TxHash(), i)
			} else {
				addrs = []stdaddr.Address{addr}
			}
			amt, err := stake.AmountFromSStxPkScrCommitment(v.PkScript)
			if err != nil {
				log.Warnf("failed to decode ticket "+
					"commitment amt output for tx hash %v"+
					", output idx %v", mtx.TxHash(), i)
			} else {
				commitAmt = &amt
			}
		} else {
			// Ignore the error here since an error means the script
			// couldn't parse and there is no additional information
			// about it anyways.
			var sc txscript.ScriptClass
			sc, addrs, reqSigs, _ = txscript.ExtractPkScriptAddrs(
				v.Version, v.PkScript, chainParams,
				isTreasuryEnabled)
			scriptClass = sc.String()
		}

		// Encode the addresses while checking if the address passes the
		// filter when needed.
		passesFilter := len(filterAddrMap) == 0
		encodedAddrs := make([]string, len(addrs))
		for j, addr := range addrs {
			encodedAddr := addr.String()
			encodedAddrs[j] = encodedAddr

			// No need to check the map again if the filter already
			// passes.
			if passesFilter {
				continue
			}
			if _, exists := filterAddrMap[encodedAddr]; exists {
				passesFilter = true
			}
		}

		if !passesFilter {
			continue
		}

		var vout chainjson.Vout
		voutSPK := &vout.ScriptPubKey
		vout.N = uint32(i)
		vout.Value = dcrutil.Amount(v.Value).ToCoin()
		vout.Version = v.Version
		voutSPK.Addresses = encodedAddrs
		voutSPK.Asm = disbuf
		voutSPK.Hex = hex.EncodeToString(v.PkScript)
		voutSPK.Type = scriptClass
		voutSPK.ReqSigs = int32(reqSigs)
		if commitAmt != nil {
			voutSPK.CommitAmt = dcrjson.Float64(commitAmt.ToCoin())
		}

		voutList = append(voutList, vout)
	}

	return voutList
}

// difficultyRatio returns the proof-of-work difficulty as a multiple of the
// minimum difficulty using the passed bits field from the header of a block.
func difficultyRatio(bits uint32, params *chaincfg.Params, log slog.Logger) float64 {
	// The minimum difficulty is the max possible proof-of-work limit bits
	// converted back to a number.  Note this is not the same as the proof
	// of work limit directly because the block difficulty is encoded in a
	// block with the compact form which loses precision.
	max := blockchain.CompactToBig(params.PowLimitBits)
	target := blockchain.CompactToBig(bits)

	difficulty := new(big.Rat).SetFrac(max, target)
	outString := difficulty.FloatString(8)
	diff, err := strconv.ParseFloat(outString, 64)
	if err != nil {
		log.Errorf("Cannot get difficulty: %v", err)
		return 0
	}
	return diff
}

// GetTransaction returns the details of a wallet tx, if the wallet contains a
// tx with the provided hash. Returns asset.CoinNotFoundError if the tx is not
// found in the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error) {
	txd, err := wallet.UnstableAPI(spvw.wallet).TxDetails(ctx, txHash)
	if errors.Is(err, errors.NotExist) {
		return nil, asset.CoinNotFoundError
	} else if err != nil {
		return nil, err
	}

	_, tipHeight := spvw.wallet.MainChainTip(ctx)

	var b strings.Builder
	b.Grow(2 * txd.MsgTx.SerializeSize())
	err = txd.MsgTx.Serialize(hex.NewEncoder(&b))
	if err != nil {
		return nil, err
	}

	getTxResult := &walletjson.GetTransactionResult{
		TxID:            txHash.String(),
		Hex:             b.String(),
		Time:            txd.Received.Unix(),
		TimeReceived:    txd.Received.Unix(),
		WalletConflicts: []string{}, // Not saved
	}

	if txd.Block.Height != -1 {
		getTxResult.BlockHash = txd.Block.Hash.String()
		getTxResult.BlockTime = txd.Block.Time.Unix()
		getTxResult.Confirmations = int64(confirms(txd.Block.Height,
			tipHeight))
	}

	var (
		debitTotal  dcrutil.Amount
		creditTotal dcrutil.Amount
		fee         dcrutil.Amount
		negFeeF64   float64
	)
	for _, deb := range txd.Debits {
		debitTotal += deb.Amount
	}
	for _, cred := range txd.Credits {
		creditTotal += cred.Amount
	}
	// Fee can only be determined if every input is a debit.
	if len(txd.Debits) == len(txd.MsgTx.TxIn) {
		var outputTotal dcrutil.Amount
		for _, output := range txd.MsgTx.TxOut {
			outputTotal += dcrutil.Amount(output.Value)
		}
		fee = debitTotal - outputTotal
		negFeeF64 = (-fee).ToCoin()
	}
	getTxResult.Amount = (creditTotal - debitTotal).ToCoin()
	getTxResult.Fee = negFeeF64

	details, err := spvw.wallet.ListTransactionDetails(ctx, txHash)
	if err != nil {
		return nil, err
	}
	getTxResult.Details = make([]walletjson.GetTransactionDetailsResult, len(details))
	for i, d := range details {
		getTxResult.Details[i] = walletjson.GetTransactionDetailsResult{
			Account:           d.Account,
			Address:           d.Address,
			Amount:            d.Amount,
			Category:          d.Category,
			InvolvesWatchOnly: d.InvolvesWatchOnly,
			Fee:               d.Fee,
			Vout:              d.Vout,
		}
	}

	return getTxResult, nil
}

// GetRawTransactionVerbose returns details of the tx with the provided hash.
// NOTE: SPV wallets are unable to look up non-wallet transactions so this will
// always return a not-supported error.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) GetRawTransactionVerbose(ctx context.Context, txHash *chainhash.Hash) (*chainjson.TxRawResult, error) {
	return nil, fmt.Errorf("getrawtransaction not supported by spv wallets")
}

// GetRawMempool returns hashes for all txs of the specified type in the node's
// mempool.
// NOTE: SPV wallets do not have a mempool so this will always return a
// not-supported error.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error) {
	return nil, fmt.Errorf("getrawmempool not supported by spv wallets")
}

// GetBestBlock returns the hash and height of the wallet's best block.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error) {
	walletBestHash, walletBestHeight := spvw.wallet.MainChainTip(ctx)
	return &walletBestHash, int64(walletBestHeight), nil
}

// GetBlockHash returns the hash of the mainchain block at the specified height.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
	id := wallet.NewBlockIdentifierFromHeight(int32(blockHeight))
	info, err := spvw.wallet.BlockInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	blockHash := info.Hash
	return &blockHash, nil
}

// BlockCFilter fetches the block filter info for the specified block.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) BlockCFilter(ctx context.Context, blockHash *chainhash.Hash) (filter, key string, err error) {
	keyB, cFilter, err := spvw.wallet.CFilterV2(ctx, blockHash)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(cFilter.Bytes()), hex.EncodeToString(keyB[:]), nil
}

// LockWallet locks the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) LockWallet(_ context.Context) error {
	// TODO: dcrlibwallet considers accountmixer status before locking the wallet
	spvw.wallet.Lock()
	return nil
}

// UnlockWallet unlocks the wallet.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) UnlockWallet(ctx context.Context, passphrase string, timeoutSecs int64) error {
	var lockAfter <-chan time.Time
	if timeoutSecs != 0 {
		timeout := time.Second * time.Duration(timeoutSecs)
		lockAfter = time.After(timeout)
	}
	return spvw.wallet.Unlock(ctx, []byte(passphrase), lockAfter)
}

// WalletUnlocked returns true if the wallet is unlocked.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) WalletUnlocked(_ context.Context) bool {
	return !spvw.wallet.Locked()
}

// AccountUnlocked returns true if the specified account is unlocked.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) AccountUnlocked(ctx context.Context, account string) (*walletjson.AccountUnlockedResult, error) {
	acctNumber, err := spvw.accountNumber(ctx, account)
	if err != nil {
		return nil, err
	}

	encrypted, err := spvw.wallet.AccountHasPassphrase(ctx, acctNumber)
	if err != nil {
		return nil, err
	}
	if !encrypted {
		return &walletjson.AccountUnlockedResult{}, nil
	}

	unlocked, err := spvw.wallet.AccountUnlocked(ctx, acctNumber)
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
func (spvw *SpvWallet) LockAccount(ctx context.Context, account string) error {
	acctNumber, err := spvw.accountNumber(ctx, account)
	if err != nil {
		return err
	}
	return spvw.wallet.LockAccount(ctx, acctNumber)
}

// UnlockAccount unlocks the specified account.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) UnlockAccount(ctx context.Context, account, passphrase string) error {
	accountNumber, err := spvw.accountNumber(ctx, account)
	if err != nil {
		return err
	}
	return spvw.wallet.UnlockAccount(ctx, accountNumber, []byte(passphrase))
}

// AddressPrivKey fetches the privkey for the specified address.
// Part of the decred.org/dcrdex/client/asset/dcr.Wallet interface.
func (spvw *SpvWallet) AddressPrivKey(ctx context.Context, address stdaddr.Address) (*dcrutil.WIF, error) {
	key, err := spvw.wallet.DumpWIFPrivateKey(ctx, address)
	if err != nil {
		if errors.Is(err, errors.Locked) {
			return nil, fmt.Errorf("wallet or account locked, unlock first")
		}
		return nil, err
	}
	return dcrutil.DecodeWIF(key, spvw.chainParams.PrivateKeyID)
}

func (spvw *SpvWallet) accountNumber(ctx context.Context, account string) (uint32, error) {
	acctNumber, err := spvw.wallet.AccountNumber(ctx, account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return 0, fmt.Errorf("%q account does not exist", account)
		}
		return 0, err
	}
	return acctNumber, nil
}

// confirms returns the number of confirmations for a transaction in a block at
// height txHeight (or -1 for an unconfirmed tx) given the chain height
// curHeight.
func confirms(txHeight, curHeight int32) int32 {
	switch {
	case txHeight == -1, txHeight > curHeight:
		return 0
	default:
		return curHeight - txHeight + 1
	}
}
