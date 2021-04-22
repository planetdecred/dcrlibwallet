package dcrlibwallet

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/planetdecred/dcrlibwallet/vsp"

	"decred.org/dcrwallet/wallet/txrules"

	w "decred.org/dcrwallet/wallet"
	"decred.org/dcrwallet/wallet/txsizes"
	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

const apiVSPInfo = "/api/v3/vspinfo"

type VSP struct {
	vspHost         string
	pubKey          []byte
	w               *Wallet
	mwRef           *MultiWallet
	purchaseAccount uint32
	changeAccount   uint32
	chainParams     *chaincfg.Params

	*vsp.Client
	ctx context.Context
	c   *w.ConfirmationNotificationsClient

	ticketToFeeLock sync.Mutex
	feesToTickets   map[chainhash.Hash]chainhash.Hash
	ticketsToFees   map[chainhash.Hash]PendingFee

	prng lockedRand
}

func (mw *MultiWallet) NewVSPClient(vspHost string, walletID int, purchaseAccount uint32) (*VSP, error) {
	sourceWallet := mw.WalletWithID(walletID)
	if sourceWallet == nil {
		return nil, fmt.Errorf("wallet doesn't exist")
	}

	source, err := vsp.CreateRandSource(cryptorand.Reader)
	if err != nil {
		panic(err)
	}

	v := &VSP{
		vspHost:         vspHost,
		w:               sourceWallet,
		mwRef:           mw,
		purchaseAccount: purchaseAccount,
		changeAccount:   purchaseAccount,
		chainParams:     mw.chainParams,
		prng: lockedRand{
			rand: source,
		},

		feesToTickets: make(map[chainhash.Hash]chainhash.Hash),
		ticketsToFees: make(map[chainhash.Hash]PendingFee),
	}

	ctx, cancel := mw.contextWithShutdownCancel()
	mw.cancelFuncs = append(mw.cancelFuncs, cancel)

	v.Client = vsp.NewClient(vspHost, nil, sourceWallet.internal)
	vspInfo, err := v.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	v.ctx = ctx
	v.c = sourceWallet.internal.NtfnServer.ConfirmationNotifications(ctx)
	v.Client.Pub = vspInfo.PubKey
	return v, nil
}

func (v *VSP) PoolFee(ctx context.Context) (float64, error) {
	var vspInfo VspInfoResponse
	err := v.Client.Get(ctx, apiVSPInfo, &vspInfo)
	if err != nil {
		return -1, err
	}
	return vspInfo.FeePercentage, nil
}

// PurchaseStakeTickets purchases the number of tickets passed as an argument and pays the fees for the tickets
func (v *VSP) PurchaseTickets(ticketCount, expiryBlocks int32, passphrase []byte) error {
	defer v.w.LockWallet()
	passphraseCopy := make([]byte, len(passphrase))
	_ = copy(passphraseCopy, passphrase)

	err := v.w.UnlockWallet(passphraseCopy)
	if err != nil {
		return translateError(err)
	}

	request := &w.PurchaseTicketsRequest{
		Count:                int(ticketCount),
		SourceAccount:        v.purchaseAccount,
		Expiry:               v.w.GetBestBlock() + expiryBlocks,
		MinConf:              DefaultRequiredConfirmations,
		VSPFeeProcess:        v.PoolFee,
		VSPFeePaymentProcess: v.ProcessFee,
	}

	networkBackend, err := v.w.internal.NetworkBackend()
	if err != nil {
		return err
	}

	_, err = v.w.internal.PurchaseTickets(v.ctx, networkBackend, request)
	if err != nil {
		log.Errorf("PURCHASE TICKET %v", err.Error())
		return err
	}

	return nil
}

// ProcessFee
func (v *VSP) ProcessFee(ctx context.Context, ticketHash *chainhash.Hash, feeTx *wire.MsgTx) error {
	var feeAmount dcrutil.Amount
	var err error
	for i := 0; i < 2; i++ {
		feeAmount, err = v.GetFeeAddress(ctx, *ticketHash)
		if err == nil {
			break
		}
		const broadcastMsg = "ticket transaction could not be broadcast"
		if err != nil && i == 0 && strings.Contains(err.Error(), broadcastMsg) {
			time.Sleep(2 * time.Minute)
		}
	}
	if err != nil {
		return err
	}

	// Reserve new outputs to pay the fee if outputs have not already been
	// reserved.  This will the the case for fee payments that were begun on
	// already purchased tickets, where the caller did not ensure that fee
	// outputs would already be reserved.
	if len(feeTx.TxIn) == 0 {
		const minconf = 1
		inputs, err := v.w.internal.ReserveOutputsForAmount(ctx, v.purchaseAccount, feeAmount, minconf)
		if err != nil {
			return fmt.Errorf("unable to reserve enough output value to "+
				"pay VSP fee for ticket %v: %w", ticketHash, err)
		}
		for _, in := range inputs {
			feeTx.AddTxIn(wire.NewTxIn(&in.OutPoint, in.PrevOut.Value, nil))
		}
		// The transaction will be added to the wallet in an unpublished
		// state, so there is no need to leave the outputs locked.
		defer func() {
			for _, in := range inputs {
				v.w.internal.UnlockOutpoint(&in.OutPoint.Hash, in.OutPoint.Index)
			}
		}()
	}

	var input int64
	for _, in := range feeTx.TxIn {
		input += in.ValueIn
	}
	if input < int64(feeAmount) {
		err := fmt.Errorf("not enough input value to pay fee: %v < %v",
			dcrutil.Amount(input), feeAmount)
		return err
	}

	err = v.CreateFeeTx(ctx, *ticketHash, feeTx)
	if err != nil {
		return err
	}
	// set fee tx as unpublished, because it will be published by the vsp.
	feeHash := feeTx.TxHash()
	err = v.w.internal.AddTransaction(ctx, feeTx, nil)
	if err != nil {
		return err
	}

	err = v.w.internal.SetPublished(ctx, &feeHash, false)
	if err != nil {
		return err
	}

	err = v.PayFee(ctx, *ticketHash, feeTx)
	if err != nil {
		return err
	}

	err = v.w.internal.UpdateVspTicketFeeToPaid(ctx, ticketHash, &feeHash)
	if err != nil {
		return err
	}
	return nil
}

type lockedRand struct {
	mu   sync.Mutex
	rand *vsp.RandSource
}

func (r *lockedRand) coinflip() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Uint32n(2) == 0
}

func (v *VSP) CreateFeeTx(ctx context.Context, ticketHash chainhash.Hash, tx *wire.MsgTx) error {
	if tx == nil {
		tx = wire.NewMsgTx()
	}

	v.ticketToFeeLock.Lock()
	feeInfo, exists := v.ticketsToFees[ticketHash]
	v.ticketToFeeLock.Unlock()
	if !exists {
		_, err := v.GetFeeAddress(ctx, ticketHash)
		if err != nil {
			return err
		}
		v.ticketToFeeLock.Lock()
		feeInfo, exists = v.ticketsToFees[ticketHash]
		v.ticketToFeeLock.Unlock()
		if !exists {
			return fmt.Errorf("failed to find fee info for ticket %v", ticketHash)
		}
	}

	// Reserve outputs to pay for fee if it hasn't already been reserved
	if len(tx.TxIn) == 0 {
		const minConf = 1
		inputs, err := v.w.internal.ReserveOutputsForAmount(ctx, v.purchaseAccount, feeInfo.FeeAmount, minConf)
		if err != nil {
			return fmt.Errorf("unable to reserve enough output value to "+
				"pay VSP fee for ticket %v: %w", ticketHash, err)
		}

		for _, in := range inputs {
			tx.AddTxIn(wire.NewTxIn(&in.OutPoint, in.PrevOut.Value, nil))
		}

		defer func() {
			for _, in := range inputs {
				v.w.internal.UnlockOutpoint(&in.OutPoint.Hash, in.OutPoint.Index)
			}
		}()
	}

	var input int64
	for _, in := range tx.TxIn {
		input += in.ValueIn
	}

	if input < int64(feeInfo.FeeAmount) {
		err := fmt.Errorf("not enough input value to pay fee: %v < %v",
			dcrutil.Amount(input), feeInfo.FeeAmount)
		return err
	}

	feeScript, err := txscript.PayToAddrScript(feeInfo.FeeAddress)
	if err != nil {
		log.Warnf("failed to generate pay to addr script for %v: %v", feeInfo.FeeAddress, err)
		return err
	}

	addr, err := v.w.internal.NewChangeAddress(ctx, v.changeAccount)
	if err != nil {
		log.Warnf("failed to get new change address: %v", err)
		return err
	}

	var changeOut *wire.TxOut
	switch addr := addr.(type) {
	case w.Address:
		vers, script := addr.PaymentScript()
		changeOut = &wire.TxOut{PkScript: script, Version: vers}
	default:
		return fmt.Errorf("failed to convert '%T' to wallet.Address", addr)
	}

	tx.TxOut = append(tx.TxOut[:0], wire.NewTxOut(int64(feeInfo.FeeAmount), feeScript))
	feeRate := v.w.internal.RelayFee()
	scriptSizes := make([]int, len(tx.TxIn))
	for i := range scriptSizes {
		scriptSizes[i] = txsizes.RedeemP2PKHSigScriptSize
	}
	est := txsizes.EstimateSerializeSize(scriptSizes, tx.TxOut, txsizes.P2PKHPkScriptSize)
	change := input
	change -= tx.TxOut[0].Value
	change -= int64(txrules.FeeForSerializeSize(feeRate, est))
	if !txrules.IsDustAmount(dcrutil.Amount(change), txsizes.P2PKHPkScriptSize, feeRate) {
		changeOut.Value = change
		tx.TxOut = append(tx.TxOut, changeOut)
		// randomize position
		if v.prng.coinflip() {
			tx.TxOut[0], tx.TxOut[1] = tx.TxOut[1], tx.TxOut[0]
		}
	}

	sigErrs, err := v.w.internal.SignTransaction(ctx, tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil {
		log.Errorf("failed to sign transaction: %v", err)
		for _, sigErr := range sigErrs {
			log.Errorf("\t%v", sigErr)
		}
		return err
	}

	return nil
}

// PayFee receives an unsigned fee tx, signs it and make a pays fee request to
// the vsp, so the ticket get registered.
func (v *VSP) PayFee(ctx context.Context, ticketHash chainhash.Hash, feeTx *wire.MsgTx) error {
	if feeTx == nil {
		return fmt.Errorf("nil fee tx")
	}

	v.ticketToFeeLock.Lock()
	feeInfo, exists := v.ticketsToFees[ticketHash]
	v.ticketToFeeLock.Unlock()
	if !exists {
		return fmt.Errorf("call GetFeeAddress first")
	}

	votingKeyWIF, err := v.w.internal.DumpWIFPrivateKey(ctx, feeInfo.VotingAddress)
	if err != nil {
		log.Errorf("failed to retrieve privkey for %v: %v", feeInfo.VotingAddress, err)
		return err
	}

	// Retrieve voting preferences
	agendaChoices, _, err := v.w.internal.AgendaChoices(ctx, &ticketHash)
	if err != nil {
		log.Errorf("failed to retrieve agenda choices for %v: %v", ticketHash, err)
		return err
	}

	voteChoices := make(map[string]string)
	for _, agendaChoice := range agendaChoices {
		voteChoices[agendaChoice.AgendaID] = agendaChoice.ChoiceID
	}

	var payfeeResponse PayFeeResponse
	requestBody, err := json.Marshal(&PayFeeRequest{
		Timestamp:   time.Now().Unix(),
		TicketHash:  ticketHash.String(),
		FeeTx:       txMarshaler(feeTx),
		VotingKey:   votingKeyWIF,
		VoteChoices: voteChoices,
	})
	if err != nil {
		return err
	}

	err = v.Client.Post(ctx, "/api/v3/payfee", feeInfo.CommitmentAddress,
		&payfeeResponse, json.RawMessage(requestBody))
	if err != nil {
		return err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, payfeeResponse.Request) {
		log.Warnf("[PAYFee] server response has differing request: %#v != %#v",
			string(requestBody), string(payfeeResponse.Request))
		return fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?
	v.ticketToFeeLock.Lock()
	feeInfo.FeeTx = feeTx
	v.ticketsToFees[ticketHash] = feeInfo
	v.ticketToFeeLock.Unlock()
	log.Infof("successfully processed %v", ticketHash)

	return nil
}

// GetInfo returns the information of the specified VSP base URL
func (v *VSP) GetInfo(ctx context.Context) (*VspInfoResponse, error) {
	var response VspInfoResponse
	err := v.Client.Get(ctx, apiVSPInfo, &response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

func (v *VSP) tx(hash *chainhash.Hash) (*wire.MsgTx, error) {
	txs, _, err := v.w.internal.GetTransactionsByHashes(v.ctx, []*chainhash.Hash{hash})
	if err != nil {
		return nil, err
	}
	return txs[0], nil
}

func (v *VSP) GetFeeAddress(ctx context.Context, ticketHash chainhash.Hash) (dcrutil.Amount, error) {
	// Fetch ticket
	txs, _, err := v.w.internal.GetTransactionsByHashes(ctx, []*chainhash.Hash{&ticketHash})
	if err != nil {
		log.Errorf("failed to retrieve ticket %v: %v", ticketHash, err)
		return 0, err
	}
	ticketTx := txs[0]

	// Fetch parent transaction
	parentHash := ticketTx.TxIn[0].PreviousOutPoint.Hash
	const scriptVersion = 0
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(scriptVersion,
		ticketTx.TxOut[0].PkScript, v.w.internal.ChainParams(), true) // Yes treasury
	if err != nil {
		log.Errorf("failed to extract stake submission address from %v: %v", ticketHash, err)
		return 0, err
	}
	if len(addrs) == 0 {
		log.Errorf("failed to get address from %v", ticketHash)
		return 0, fmt.Errorf("failed to get address from %v", ticketHash)
	}
	votingAddress := addrs[0]

	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, v.chainParams)
	if err != nil {
		log.Errorf("failed to extract script addr from %v: %v", ticketHash, err)
		return 0, err
	}

	// Fetch ticket and its parent transaction (typically, a split
	// transaction).
	parent, err := v.tx(&parentHash)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve parent %v of ticket: %w",
			parentHash, err)
	}

	ticket, err := v.tx(&ticketHash)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve ticket: %w", err)
	}

	var feeResponse FeeAddressResponse
	requestBody, err := json.Marshal(&FeeAddressRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash.String(),
		TicketHex:  txMarshaler(ticket),
		ParentHex:  txMarshaler(parent),
	})
	if err != nil {
		return 0, err
	}
	err = v.Client.Post(ctx, "/api/v3/feeaddress", commitmentAddr, &feeResponse,
		json.RawMessage(requestBody))
	if err != nil {
		return 0, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, feeResponse.Request) {
		log.Warnf("[GetFeeAddress] server response has differing request: %#v != %#v",
			string(requestBody), string(feeResponse.Request))
		return 0, fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?

	feeAddress, err := dcrutil.DecodeAddress(feeResponse.FeeAddress, v.w.internal.ChainParams())
	if err != nil {
		log.Warnf("server fee address invalid: %v", err)
		return 0, fmt.Errorf("server fee address invalid: %v", err)
	}
	feeAmount := dcrutil.Amount(feeResponse.FeeAmount)

	// TODO - convert to vsp.maxfee config option
	maxFee, err := dcrutil.NewAmount(0.1)
	if err != nil {
		return 0, err
	}

	if feeAmount > maxFee {
		log.Warnf("fee amount too high: %v > %v", feeAmount, maxFee)
		return 0, fmt.Errorf("server fee amount too high: %v > %v", feeAmount, maxFee)
	}

	v.ticketToFeeLock.Lock()
	v.ticketsToFees[ticketHash] = PendingFee{
		CommitmentAddress: commitmentAddr,
		VotingAddress:     votingAddress,
		FeeAddress:        feeAddress,
		FeeAmount:         feeAmount,
	}
	v.ticketToFeeLock.Unlock()

	return feeAmount, nil
}

type marshaler struct {
	marshaled []byte
	err       error
}

func (m *marshaler) MarshalJSON() ([]byte, error) {
	return m.marshaled, m.err
}

func txMarshaler(tx *wire.MsgTx) json.Marshaler {
	var buf bytes.Buffer
	buf.Grow(2 + tx.SerializeSize()*2)
	buf.WriteByte('"')
	err := tx.Serialize(hex.NewEncoder(&buf))
	buf.WriteByte('"')
	return &marshaler{buf.Bytes(), err}
}
