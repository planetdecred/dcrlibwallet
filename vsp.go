package dcrlibwallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	"decred.org/dcrwallet/v2/errors"

	w "decred.org/dcrwallet/v2/wallet"
	"decred.org/dcrwallet/v2/wallet/txauthor"
	"decred.org/dcrwallet/v2/wallet/txrules"
	"decred.org/dcrwallet/v2/wallet/txsizes"

	"github.com/decred/dcrd/blockchain/stake/v4"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
)

const apiVSPInfo = "/api/v3/vspinfo"

type VSP struct {
	w               *Wallet
	purchaseAccount uint32
	changeAccount   uint32
	chainParams     *chaincfg.Params

	client *vspClient
	ctx    context.Context

	ticketToFeeLock sync.Mutex
	ticketsToFees   map[chainhash.Hash]PendingFee
}

// NewVSPClient starts a new vsp client
func (mw *MultiWallet) NewVSPClient(vspHost string, walletID int, purchaseAccount uint32) (*VSP, error) {
	sourceWallet := mw.WalletWithID(walletID)
	if sourceWallet == nil {
		return nil, fmt.Errorf("wallet doesn't exist")
	}

	vsp := &VSP{
		w:               sourceWallet,
		purchaseAccount: purchaseAccount,
		changeAccount:   purchaseAccount,
		chainParams:     mw.chainParams,

		ticketsToFees: make(map[chainhash.Hash]PendingFee),
	}

	ctx, _ := mw.contextWithShutdownCancel()

	vsp.ctx = ctx
	vsp.client = newVSPClient(vspHost, nil, sourceWallet.Internal())
	vspInfo, err := vsp.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	vsp.client.pub = vspInfo.PubKey
	return vsp, nil
}

//  PoolFee returns the vsp fee percentage.
func (vsp *VSP) PoolFee(ctx context.Context) (float64, error) {
	var vspInfo VspInfoResponse
	err := vsp.client.get(ctx, apiVSPInfo, &vspInfo)
	if err != nil {
		return -1, err
	}
	return vspInfo.FeePercentage, nil
}

// PurchaseStakeTickets purchases the number of tickets passed as an argument and pays the fees for the tickets
func (vsp *VSP) PurchaseTickets(ticketCount, expiryBlocks int32, passphrase []byte) error {
	err := vsp.w.UnlockWallet(passphrase)
	if err != nil {
		return translateError(err)
	}
	defer vsp.w.LockWallet()

	request := &w.PurchaseTicketsRequest{
		Count:                int(ticketCount),
		SourceAccount:        vsp.purchaseAccount,
		Expiry:               vsp.w.GetBestBlock() + expiryBlocks,
		MinConf:              DefaultRequiredConfirmations,
		VSPFeeProcess:        vsp.PoolFee,
		VSPFeePaymentProcess: vsp.ProcessFee,
	}

	networkBackend, err := vsp.w.Internal().NetworkBackend()
	if err != nil {
		return err
	}

	_, err = vsp.w.Internal().PurchaseTickets(vsp.ctx, networkBackend, request)
	if err != nil {
		return err
	}

	return nil
}

// StartTicketBuyer starts the automatic ticket buyer.
func (vsp *VSP) StartTicketBuyer(balanceToMaintain int64, passphrase []byte) error {
	if balanceToMaintain < 0 {
		return errors.New("Negative balance to maintain given")
	}

	vsp.w.cancelAutoTicketBuyerMux.Lock()
	cancelFunc := vsp.w.cancelAutoTicketBuyer
	vsp.w.cancelAutoTicketBuyerMux.Unlock()
	if cancelFunc != nil {
		return errors.New("Ticket buyer already running")
	}

	ctx, cancel := vsp.w.shutdownContextWithCancel()

	vsp.w.cancelAutoTicketBuyerMux.Lock()
	vsp.w.cancelAutoTicketBuyer = cancel
	vsp.w.cancelAutoTicketBuyerMux.Unlock()

	if len(passphrase) > 0 && vsp.w.IsLocked() {
		err := vsp.w.UnlockWallet(passphrase)
		if err != nil {
			return translateError(err)
		}
		defer vsp.w.LockWallet()
	}

	go func() {
		log.Infof("Running ticket buyer on Wallet: %s", vsp.w.Name)

		err := vsp.runTicketBuyer(ctx, balanceToMaintain, passphrase)
		if err != nil {
			if ctx.Err() != nil {
				log.Errorf("[%d] TicketBuyer instance canceled, account number: %s", vsp.w.ID, vsp.w.Name)
			}
			log.Errorf("[%d] Ticket buyer instance errored: %v", vsp.w.ID, err)
		}

		vsp.w.cancelAutoTicketBuyerMux.Lock()
		vsp.w.cancelAutoTicketBuyer = nil
		vsp.w.cancelAutoTicketBuyerMux.Unlock()
	}()

	return nil
}

// runTicketBuyer executes the ticket buyer. If the private passphrase is incorrect,
// or ever becomes incorrect due to a wallet passphrase change, runTicketBuyer exits with an
// errors.Passphrase error.
func (vsp *VSP) runTicketBuyer(ctx context.Context, balanceToMaintain int64, passphrase []byte) error {
	if len(passphrase) > 0 && vsp.w.IsLocked() {
		err := vsp.w.UnlockWallet(passphrase)
		if err != nil {
			return translateError(err)
		}
	}

	c := vsp.w.Internal().NtfnServer.MainTipChangedNotifications()
	defer c.Done()

	ctx, outerCancel := context.WithCancel(ctx)
	defer outerCancel()
	var fatal error
	var fatalMu sync.Mutex

	var nextIntervalStart, expiry int32
	var cancels []func()
	for {
		select {
		case <-ctx.Done():
			defer outerCancel()
			fatalMu.Lock()
			err := fatal
			fatalMu.Unlock()
			if err != nil {
				return err
			}
			return ctx.Err()
		case n := <-c.C:
			if len(n.AttachedBlocks) == 0 {
				continue
			}

			tip := n.AttachedBlocks[len(n.AttachedBlocks)-1]
			w := vsp.w.Internal()

			// Don't perform any actions while transactions are not synced through
			// the tip block.
			rp, err := w.RescanPoint(ctx)
			if err != nil {
				return err
			}
			if rp != nil {
				log.Debugf("Skipping autobuyer actions: transactions are not synced on Wallet: %s", vsp.w.Name)
				continue
			}

			tipHeader, err := w.BlockHeader(ctx, tip)
			if err != nil {
				log.Error(err)
				continue
			}
			height := int32(tipHeader.Height)

			// Cancel any ongoing ticket purchases which are buying
			// at an old ticket price or are no longer able to
			// create mined tickets the window.
			if height+2 >= nextIntervalStart {
				for i, cancel := range cancels {
					cancel()
					cancels[i] = nil
				}
				cancels = cancels[:0]

				intervalSize := int32(w.ChainParams().StakeDiffWindowSize)
				currentInterval := height / intervalSize
				nextIntervalStart = (currentInterval + 1) * intervalSize

				// Skip this purchase when no more tickets may be purchased in the interval and
				// the next sdiff is unknown.  The earliest any ticket may be mined is two
				// blocks from now, with the next block containing the split transaction
				// that the ticket purchase spends.
				if height+2 == nextIntervalStart {
					log.Debugf("Skipping purchase: next sdiff interval starts soon")
					continue
				}
				// Set expiry to prevent tickets from being mined in the next
				// sdiff interval.  When the next block begins the new interval,
				// the ticket is being purchased for the next interval; therefore
				// increment expiry by a full sdiff window size to prevent it
				// being mined in the interval after the next.
				expiry = nextIntervalStart
				if height+1 == nextIntervalStart {
					expiry += intervalSize
				}
			}

			wal := vsp.w
			// Get the account balance to determine how many tickets to buy
			bal, err := wal.GetAccountBalance(int32(vsp.purchaseAccount))
			if err != nil {
				return err
			}

			spendable := bal.Spendable
			if spendable < balanceToMaintain {
				log.Debugf("Skipping purchase: low available balance on Wallet: %s", wal.Name)
				return nil
			}

			spendable -= balanceToMaintain
			sdiff, err := wal.Internal().NextStakeDifficultyAfterHeader(ctx, tipHeader)
			if err != nil {
				return err
			}

			buy := int(dcrutil.Amount(spendable) / sdiff)
			if buy == 0 {
				log.Debugf("Skipping purchase: low available balance on Wallet: %s", wal.Name)
				return nil
			}

			cancelCtx, cancel := context.WithCancel(ctx)
			cancels = append(cancels, cancel)
			buyTickets := func() {
				err := vsp.buyTickets(cancelCtx, passphrase, sdiff, expiry)
				if err != nil {
					switch {
					// silence these errors
					case errors.Is(err, errors.InsufficientBalance):
					case errors.Is(err, context.Canceled):
					case errors.Is(err, context.DeadlineExceeded):
					default:
						log.Errorf("[%d] Ticket purchasing failed: %v", wal.ID, err)
					}
					if errors.Is(err, errors.Passphrase) {
						fatalMu.Lock()
						fatal = err
						fatalMu.Unlock()
						outerCancel()
					}
				}
			}

			// start separate ticket purchase for as many tickets that can be purchased
			// each purchase only buy 1 ticket.
			for i := 0; i < buy; i++ {
				go buyTickets()
			}
		}
	}
}

// buyTickets purchases one or more tickets depending on the spendable balance of
// the selected account and the specified minimum balance to maintain.
func (vsp *VSP) buyTickets(ctx context.Context, passphrase []byte, sdiff dcrutil.Amount, expiry int32) error {
	ctx, task := trace.NewTask(ctx, "ticketbuyer.buy")
	defer task.End()

	wal := vsp.w

	if len(passphrase) > 0 && wal.IsLocked() {
		err := wal.UnlockWallet(passphrase)
		if err != nil {
			return translateError(err)
		}
	}

	request := &w.PurchaseTicketsRequest{
		Count:                1, // prevent combining multi ticket in one transaction. Buy max 1 per txns
		SourceAccount:        vsp.purchaseAccount,
		Expiry:               expiry,
		MinConf:              wal.RequiredConfirmations(),
		VSPFeeProcess:        vsp.PoolFee,
		VSPFeePaymentProcess: vsp.ProcessFee,
	}

	networkBackend, err := wal.Internal().NetworkBackend()
	if err != nil {
		return err
	}

	tix, err := wal.Internal().PurchaseTickets(ctx, networkBackend, request)
	if tix != nil {
		for _, hash := range tix.TicketHashes {
			log.Infof("Purchased ticket %v at stake difficulty %v from wallet: %s", hash, sdiff, wal.Name)
		}
	}

	return err
}

// IsAutoTicketsPurchaseActive returns true if ticket buyer is active
func (wallet *Wallet) IsAutoTicketsPurchaseActive() bool {
	wallet.cancelAutoTicketBuyerMux.Lock()
	defer wallet.cancelAutoTicketBuyerMux.Unlock()
	return wallet.cancelAutoTicketBuyer != nil
}

// StopAutoTicketsPurchase stops the active ticket buyer
func (mw *MultiWallet) StopAutoTicketsPurchase(walletID int) error {
	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	wallet.cancelAutoTicketBuyerMux.Lock()
	cancelFunc := wallet.cancelAutoTicketBuyer
	wallet.cancelAutoTicketBuyerMux.Unlock()
	if cancelFunc == nil {
		return errors.New(ErrInvalid)
	}

	wallet.cancelAutoTicketBuyerMux.Lock()
	wallet.cancelAutoTicketBuyer()
	wallet.cancelAutoTicketBuyer = nil
	wallet.cancelAutoTicketBuyerMux.Unlock()
	return nil
}

// ProcessFee
func (vsp *VSP) ProcessFee(ctx context.Context, ticketHash *chainhash.Hash, feeTx *wire.MsgTx) error {
	var feeAmount dcrutil.Amount
	var err error
	for i := 0; i < 2; i++ {
		feeAmount, err = vsp.GetFeeAddress(ctx, *ticketHash)
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
		inputs, err := vsp.w.Internal().ReserveOutputsForAmount(ctx, vsp.purchaseAccount, feeAmount, minconf)
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
				vsp.w.Internal().UnlockOutpoint(&in.OutPoint.Hash, in.OutPoint.Index)
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

	err = vsp.CreateFeeTx(ctx, *ticketHash, feeTx)
	if err != nil {
		return err
	}
	// set fee tx as unpublished, because it will be published by the vsp.
	feeHash := feeTx.TxHash()
	err = vsp.w.Internal().AddTransaction(ctx, feeTx, nil)
	if err != nil {
		return err
	}

	err = vsp.w.Internal().SetPublished(ctx, &feeHash, false)
	if err != nil {
		return err
	}

	err = vsp.PayFee(ctx, *ticketHash, feeTx)
	if err != nil {
		return err
	}

	err = vsp.w.Internal().UpdateVspTicketFeeToPaid(ctx, ticketHash, &feeHash, vsp.client.url, vsp.client.pub)
	if err != nil {
		return err
	}
	return nil
}

func (vsp *VSP) CreateFeeTx(ctx context.Context, ticketHash chainhash.Hash, tx *wire.MsgTx) error {
	if tx == nil {
		tx = wire.NewMsgTx()
	}

	vsp.ticketToFeeLock.Lock()
	feeInfo, exists := vsp.ticketsToFees[ticketHash]
	vsp.ticketToFeeLock.Unlock()
	if !exists {
		_, err := vsp.GetFeeAddress(ctx, ticketHash)
		if err != nil {
			return err
		}
		vsp.ticketToFeeLock.Lock()
		feeInfo, exists = vsp.ticketsToFees[ticketHash]
		vsp.ticketToFeeLock.Unlock()
		if !exists {
			return fmt.Errorf("failed to find fee info for ticket %v", ticketHash)
		}
	}

	// Reserve outputs to pay for fee if it hasn't already been reserved
	if len(tx.TxIn) == 0 {
		const minConf = 1
		inputs, err := vsp.w.Internal().ReserveOutputsForAmount(ctx, vsp.purchaseAccount, feeInfo.FeeAmount, minConf)
		if err != nil {
			return fmt.Errorf("unable to reserve enough output value to "+
				"pay VSP fee for ticket %v: %w", ticketHash, err)
		}

		for _, in := range inputs {
			tx.AddTxIn(wire.NewTxIn(&in.OutPoint, in.PrevOut.Value, nil))
		}

		defer func() {
			for _, in := range inputs {
				vsp.w.Internal().UnlockOutpoint(&in.OutPoint.Hash, in.OutPoint.Index)
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

	_, feeScript := feeInfo.FeeAddress.PaymentScript()
	addr, err := vsp.w.Internal().NewChangeAddress(ctx, vsp.changeAccount)
	if err != nil {
		return errors.Errorf("failed to get new change address: %v", err)
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
	feeRate := vsp.w.Internal().RelayFee()
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
		txauthor.RandomizeOutputPosition(tx.TxOut, 0)
	}

	sigErrs, err := vsp.w.Internal().SignTransaction(ctx, tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil {
		for _, sigErr := range sigErrs {
			log.Errorf("\t%v", sigErr)
		}
		return errors.Errorf("failed to sign transaction: %v", err)
	}

	return nil
}

// PayFee receives an unsigned fee tx, signs it and make a pays fee request to
// the vsp, so the ticket get registered.
func (vsp *VSP) PayFee(ctx context.Context, ticketHash chainhash.Hash, feeTx *wire.MsgTx) error {
	if feeTx == nil {
		return fmt.Errorf("nil fee tx")
	}

	vsp.ticketToFeeLock.Lock()
	feeInfo, exists := vsp.ticketsToFees[ticketHash]
	vsp.ticketToFeeLock.Unlock()
	if !exists {
		return fmt.Errorf("call GetFeeAddress first")
	}

	votingKeyWIF, err := vsp.w.Internal().DumpWIFPrivateKey(ctx, feeInfo.VotingAddress)
	if err != nil {
		return errors.Errorf("failed to retrieve privkey for %v: %v", feeInfo.VotingAddress, err)
	}

	// Retrieve voting preferences
	agendaChoices, _, err := vsp.w.Internal().AgendaChoices(ctx, &ticketHash)
	if err != nil {
		return errors.Errorf("failed to retrieve agenda choices for %v: %v", ticketHash, err)
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

	err = vsp.client.post(ctx, "/api/v3/payfee", feeInfo.CommitmentAddress,
		&payfeeResponse, json.RawMessage(requestBody))
	if err != nil {
		return err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, payfeeResponse.Request) {
		return fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?
	vsp.ticketToFeeLock.Lock()
	feeInfo.FeeTx = feeTx
	vsp.ticketsToFees[ticketHash] = feeInfo
	vsp.ticketToFeeLock.Unlock()

	return nil
}

// GetInfo returns the information of the specified VSP base URL
func (vsp *VSP) GetInfo(ctx context.Context) (*VspInfoResponse, error) {
	var response VspInfoResponse
	err := vsp.client.get(ctx, apiVSPInfo, &response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

func (vsp *VSP) tx(hash *chainhash.Hash) (*wire.MsgTx, error) {
	txs, _, err := vsp.w.Internal().GetTransactionsByHashes(vsp.ctx, []*chainhash.Hash{hash})
	if err != nil {
		return nil, err
	}
	return txs[0], nil
}

func (vsp *VSP) GetFeeAddress(ctx context.Context, ticketHash chainhash.Hash) (dcrutil.Amount, error) {
	// Fetch ticket
	txs, _, err := vsp.w.Internal().GetTransactionsByHashes(ctx, []*chainhash.Hash{&ticketHash})
	if err != nil {
		return 0, errors.Errorf("failed to retrieve ticket %v: %v", ticketHash, err)
	}
	ticketTx := txs[0]

	// Fetch parent transaction
	parentHash := ticketTx.TxIn[0].PreviousOutPoint.Hash
	const scriptVersion = 0
	_, addrs := stdscript.ExtractAddrs(scriptVersion, ticketTx.TxOut[0].PkScript, vsp.w.Internal().ChainParams())
	if err != nil {
		return 0, errors.Errorf("failed to extract stake submission address from %v: %v", ticketHash, err)
	}
	if len(addrs) == 0 {
		return 0, errors.Errorf("failed to get address from %v", ticketHash)
	}
	votingAddress := addrs[0]

	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, vsp.chainParams)
	if err != nil {
		return 0, errors.Errorf("failed to extract script addr from %v: %v", ticketHash, err)
	}

	// Fetch ticket and its parent transaction (typically, a split
	// transaction).
	parent, err := vsp.tx(&parentHash)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve parent %v of ticket: %w",
			parentHash, err)
	}

	ticket, err := vsp.tx(&ticketHash)
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
	err = vsp.client.post(ctx, "/api/v3/feeaddress", commitmentAddr, &feeResponse,
		json.RawMessage(requestBody))
	if err != nil {
		return 0, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, feeResponse.Request) {
		return 0, fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?

	feeAddress, err := stdaddr.DecodeAddress(feeResponse.FeeAddress, vsp.w.Internal().ChainParams())
	if err != nil {
		return 0, fmt.Errorf("server fee address invalid: %v", err)
	}
	feeAmount := dcrutil.Amount(feeResponse.FeeAmount)

	// TODO - convert to vsp.maxfee config option
	maxFee, err := dcrutil.NewAmount(0.1)
	if err != nil {
		return 0, err
	}

	if feeAmount > maxFee {
		return 0, fmt.Errorf("server fee amount too high: %v > %v", feeAmount, maxFee)
	}

	vsp.ticketToFeeLock.Lock()
	vsp.ticketsToFees[ticketHash] = PendingFee{
		CommitmentAddress: commitmentAddr,
		VotingAddress:     votingAddress,
		FeeAddress:        feeAddress,
		FeeAmount:         feeAmount,
	}
	vsp.ticketToFeeLock.Unlock()

	return feeAmount, nil
}

type valueOut struct {
	Remember string
	List     []string
}

func (mw *MultiWallet) GetVSPList(net string) {
	var valueOut valueOut

	mw.ReadUserConfigValue(VSPHostConfigKey, &valueOut)
	var loadedVSP []*VSPInfo

	for _, host := range valueOut.List {
		info, err := mw.getInfo(host)
		if err == nil {
			loadedVSP = append(loadedVSP, &VSPInfo{
				Host: host,
				Info: info,
			})
		}
	}

	l, _ := getInitVSPInfo("https://api.decred.org/?c=vsp")
	for h, v := range l {
		if strings.Contains(net, v.Network) {
			loadedVSP = append(loadedVSP, &VSPInfo{
				Host: fmt.Sprintf("https://%s", h),
				Info: v,
			})
		}
	}

	mw.VspList = loadedVSP
}

func (mw *MultiWallet) AddVSP(host, net string) error {
	var valueOut valueOut

	// check if host already exists
	_ = mw.ReadUserConfigValue(VSPHostConfigKey, &valueOut)
	for _, vsp := range valueOut.List {
		if vsp == host {
			return fmt.Errorf("existing host %s", host)
		}
	}

	// validate host network
	info, err := mw.getInfo(host)
	if err != nil {
		return err
	}

	if info.Network != net {
		return fmt.Errorf("invalid net %s", info.Network)
	}

	valueOut.List = append(valueOut.List, host)
	mw.SaveUserConfigValue(VSPHostConfigKey, valueOut)
	mw.VspList = append(mw.VspList, &VSPInfo{
		Host: host,
		Info: info,
	})

	return nil
}

func (mw *MultiWallet) GetRememberVSP() string {
	var valueOut valueOut

	mw.ReadUserConfigValue(VSPHostConfigKey, &valueOut)
	return valueOut.Remember
}

func (mw *MultiWallet) RememberVSP(host string) {
	var valueOut valueOut

	err := mw.ReadUserConfigValue(VSPHostConfigKey, &valueOut)
	if err != nil {
		log.Error(err.Error())
	}

	valueOut.Remember = host
	mw.SaveUserConfigValue(VSPHostConfigKey, valueOut)
}

func (mw *MultiWallet) getInfo(host string) (*VspInfoResponse, error) {
	sourceWallet := mw.WalletWithID(mw.OpenedWalletIDsRaw()[0])
	if sourceWallet == nil {
		return nil, fmt.Errorf("wallet doesn't exist")
	}

	ctx, cancel := mw.contextWithShutdownCancel()
	mw.cancelFuncs = append(mw.cancelFuncs, cancel)

	vsp := &VSP{}
	vsp.client = newVSPClient(host, nil, sourceWallet.Internal())
	vspInfo, err := vsp.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	return vspInfo, nil
}

// SetAutoTicketsBuyerConfig set ticket buyer configuration for a particular wallet
func (wallet *Wallet) SetAutoTicketsBuyerConfig(vspHost string, purchaseAccount int32, amountToMaintain int64) {
	wallet.SetLongConfigValueForKey(TicketBuyerATMConfigKey, amountToMaintain)
	wallet.SetInt32ConfigValueForKey(TicketBuyerAccountConfigKey, purchaseAccount)
	wallet.SetStringConfigValueForKey(TicketBuyerVSPHostConfigKey, vspHost)
}

// GetAutoTicketsBuyerConfig gets ticekt buyer config for a selected wallet
func (wallet *Wallet) GetAutoTicketsBuyerConfig() *TicketBuyerConfig {
	btm := wallet.ReadLongConfigValueForKey(TicketBuyerATMConfigKey, -1)
	accNum := wallet.ReadInt32ConfigValueForKey(TicketBuyerAccountConfigKey, -1)
	vspHost := wallet.ReadStringConfigValueForKey(TicketBuyerVSPHostConfigKey, "")

	return &TicketBuyerConfig{
		VspHost:           vspHost,
		PurchaseAccount:   accNum,
		BalanceToMaintain: btm,
	}
}

// TicketBuyerConfigIsSet checks if ticket buyer config has been set for a
// selected wallet
func (wallet *Wallet) TicketBuyerConfigIsSet() bool {
	return wallet.ReadStringConfigValueForKey(TicketBuyerVSPHostConfigKey, "") != ""
}

// ClearTicketBuyerConfig clear all save ticket buyer config for a selected wallet
func (mw *MultiWallet) ClearTicketBuyerConfig(walletID int) error {
	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	mw.SetLongConfigValueForKey(TicketBuyerATMConfigKey, -1)
	mw.SetInt32ConfigValueForKey(TicketBuyerAccountConfigKey, -1)
	mw.SetStringConfigValueForKey(TicketBuyerVSPHostConfigKey, "")

	return nil
}

// getInitVSPInfo returns the list information of the VSP
func getInitVSPInfo(url string) (map[string]*VspInfoResponse, error) {
	rq := new(http.Client)
	resp, err := rq.Get((url))
	if err != nil {
		return nil, err
	}

	b, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non 200 response from server: %v", string(b))
	}

	var vspInfoResponse map[string]*VspInfoResponse
	err = json.Unmarshal(b, &vspInfoResponse)
	if err != nil {
		return nil, err
	}

	return vspInfoResponse, nil
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
