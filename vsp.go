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

	*vspClient
	ctx context.Context

	ticketToFeeLock sync.Mutex
	ticketsToFees   map[chainhash.Hash]PendingFee
}

func (mw *MultiWallet) NewVSPClient(vspHost string, walletID int, purchaseAccount uint32) (*VSP, error) {
	sourceWallet := mw.WalletWithID(walletID)
	if sourceWallet == nil {
		return nil, fmt.Errorf("wallet doesn't exist")
	}

	v := &VSP{
		w:               sourceWallet,
		purchaseAccount: purchaseAccount,
		changeAccount:   purchaseAccount,
		chainParams:     mw.chainParams,

		ticketsToFees: make(map[chainhash.Hash]PendingFee),
	}

	ctx, cancel := mw.contextWithShutdownCancel()
	mw.cancelFuncs = append(mw.cancelFuncs, cancel)

	v.vspClient = newVSPClient(vspHost, nil, sourceWallet.Internal())
	vspInfo, err := v.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	v.ctx = ctx
	v.vspClient.pub = vspInfo.PubKey
	return v, nil
}

func (v *VSP) PoolFee(ctx context.Context) (float64, error) {
	var vspInfo VspInfoResponse
	err := v.vspClient.get(ctx, apiVSPInfo, &vspInfo)
	if err != nil {
		return -1, err
	}
	return vspInfo.FeePercentage, nil
}

// PurchaseStakeTickets purchases the number of tickets passed as an argument and pays the fees for the tickets
func (v *VSP) PurchaseTickets(ticketCount, expiryBlocks int32, passphrase []byte) error {
	err := v.w.UnlockWallet(passphrase)
	if err != nil {
		return translateError(err)
	}
	defer v.w.LockWallet()

	request := &w.PurchaseTicketsRequest{
		Count:                int(ticketCount),
		SourceAccount:        v.purchaseAccount,
		Expiry:               v.w.GetBestBlock() + expiryBlocks,
		MinConf:              DefaultRequiredConfirmations,
		VSPFeeProcess:        v.PoolFee,
		VSPFeePaymentProcess: v.ProcessFee,
	}

	networkBackend, err := v.w.Internal().NetworkBackend()
	if err != nil {
		return err
	}

	_, err = v.w.Internal().PurchaseTickets(v.ctx, networkBackend, request)
	if err != nil {
		return err
	}

	return nil
}

// AutoTicketsPurchase purchases the number of tickets passed as an argument and pays the fees for the tickets
func (v *VSP) AutoTicketsPurchase(balanceToMaintain int64, passphrase []byte) error {
	err := v.w.UnlockWallet(passphrase)
	if err != nil {
		return translateError(err)
	}
	defer v.w.LockWallet()

	ctx, outerCancel := context.WithCancel(v.ctx)
	v.w.cancelAutoTicketBuyer = make([]context.CancelFunc, 0)

	defer outerCancel()
	var fatal error
	var fatalMu sync.Mutex

	c := v.w.Internal().NtfnServer.MainTipChangedNotifications()
	defer c.Done()

	var nextIntervalStart, expiry int32

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
			w := v.w.Internal()

			// Don't perform any actions while transactions are not synced through
			// the tip block.
			rp, err := w.RescanPoint(ctx)
			if err != nil {
				return err
			}
			if rp != nil {
				log.Debugf("Skipping autobuyer actions: transactions are not synced")
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
				for i, cancel := range v.w.cancelAutoTicketBuyer {
					cancel()
					v.w.cancelAutoTicketBuyer[i] = nil
				}
				v.w.cancelAutoTicketBuyer = v.w.cancelAutoTicketBuyer[:0]

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

			cancelCtx, cancel := context.WithCancel(ctx)
			v.w.cancelAutoTicketBuyer = append(v.w.cancelAutoTicketBuyer, cancel)

			go func() {
				err := v.buyTicket(cancelCtx, passphrase, tipHeader, expiry, balanceToMaintain)
				if err != nil {
					switch {
					// silence these errors
					case errors.Is(err, errors.InsufficientBalance):
					case errors.Is(err, context.Canceled):
					case errors.Is(err, context.DeadlineExceeded):
					default:
						log.Errorf("Ticket purchasing failed: %v", err)
					}
					if errors.Is(err, errors.Passphrase) {
						fatalMu.Lock()
						fatal = err
						fatalMu.Unlock()
						outerCancel()
					}
				}
			}()
		}
	}
}

// buyTicket calculate the number of tickets before purchase
func (v *VSP) buyTicket(ctx context.Context, passphrase []byte, tip *wire.BlockHeader, expiry int32, maintain int64) error {
	ctx, task := trace.NewTask(ctx, "ticketbuyer.buy")
	defer task.End()

	wal := v.w

	// Determine how many tickets to buy
	bal, err := wal.GetAccountBalance(int32(v.purchaseAccount))
	if err != nil {
		return err
	}
	spendable := bal.Spendable

	if spendable < maintain {
		log.Debugf("Skipping purchase: low available balance")
		return nil
	}
	spendable -= maintain
	sdiff, err := wal.Internal().NextStakeDifficultyAfterHeader(ctx, tip)
	if err != nil {
		return err
	}
	buy := int(dcrutil.Amount(spendable) / sdiff)
	if buy == 0 {
		log.Debugf("Skipping purchase: low available balance")
		return nil
	}
	max := int(wal.Internal().ChainParams().MaxFreshStakePerBlock)
	if buy > max {
		buy = max
	}

	request := &w.PurchaseTicketsRequest{
		Count:                buy,
		SourceAccount:        v.purchaseAccount,
		Expiry:               expiry,
		MinConf:              DefaultRequiredConfirmations,
		VSPFeeProcess:        v.PoolFee,
		VSPFeePaymentProcess: v.ProcessFee,
	}

	networkBackend, err := v.w.Internal().NetworkBackend()
	if err != nil {
		return err
	}

	_, err = v.w.Internal().PurchaseTickets(ctx, networkBackend, request)
	if err != nil {
		return err
	}

	return nil
}

// IsAutoTicketsPurchaseActive returns true if account mixer is active
func (v *VSP) IsAutoTicketsPurchaseActive() bool {
	var ticketBuyerRunning bool
	for i := range v.w.cancelAutoTicketBuyer {
		ticketBuyerRunning = v.w.cancelAutoTicketBuyer[i] != nil
	}

	return ticketBuyerRunning
}

// StopAutoTicketsPurchase stops the active account mixer
func (v *VSP) StopAutoTicketsPurchase() error {
	for i, cancel := range v.w.cancelAutoTicketBuyer {
		if v.w.cancelAutoTicketBuyer == nil {
			fmt.Println("not running")
			return errors.New(ErrInvalid)
		}

		cancel()
		v.w.cancelAutoTicketBuyer[i] = nil
	}

	v.w.cancelAutoTicketBuyer = v.w.cancelAutoTicketBuyer[:0]

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
		inputs, err := v.w.Internal().ReserveOutputsForAmount(ctx, v.purchaseAccount, feeAmount, minconf)
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
				v.w.Internal().UnlockOutpoint(&in.OutPoint.Hash, in.OutPoint.Index)
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
	err = v.w.Internal().AddTransaction(ctx, feeTx, nil)
	if err != nil {
		return err
	}

	err = v.w.Internal().SetPublished(ctx, &feeHash, false)
	if err != nil {
		return err
	}

	err = v.PayFee(ctx, *ticketHash, feeTx)
	if err != nil {
		return err
	}

	err = v.w.Internal().UpdateVspTicketFeeToPaid(ctx, ticketHash, &feeHash, v.url, v.pub)
	if err != nil {
		return err
	}
	return nil
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
		inputs, err := v.w.Internal().ReserveOutputsForAmount(ctx, v.purchaseAccount, feeInfo.FeeAmount, minConf)
		if err != nil {
			return fmt.Errorf("unable to reserve enough output value to "+
				"pay VSP fee for ticket %v: %w", ticketHash, err)
		}

		for _, in := range inputs {
			tx.AddTxIn(wire.NewTxIn(&in.OutPoint, in.PrevOut.Value, nil))
		}

		defer func() {
			for _, in := range inputs {
				v.w.Internal().UnlockOutpoint(&in.OutPoint.Hash, in.OutPoint.Index)
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
	addr, err := v.w.Internal().NewChangeAddress(ctx, v.changeAccount)
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
	feeRate := v.w.Internal().RelayFee()
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

	sigErrs, err := v.w.Internal().SignTransaction(ctx, tx, txscript.SigHashAll, nil, nil, nil)
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

	votingKeyWIF, err := v.w.Internal().DumpWIFPrivateKey(ctx, feeInfo.VotingAddress)
	if err != nil {
		return errors.Errorf("failed to retrieve privkey for %v: %v", feeInfo.VotingAddress, err)
	}

	// Retrieve voting preferences
	agendaChoices, _, err := v.w.Internal().AgendaChoices(ctx, &ticketHash)
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

	err = v.vspClient.post(ctx, "/api/v3/payfee", feeInfo.CommitmentAddress,
		&payfeeResponse, json.RawMessage(requestBody))
	if err != nil {
		return err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, payfeeResponse.Request) {
		return fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?
	v.ticketToFeeLock.Lock()
	feeInfo.FeeTx = feeTx
	v.ticketsToFees[ticketHash] = feeInfo
	v.ticketToFeeLock.Unlock()

	return nil
}

// GetInfo returns the information of the specified VSP base URL
func (v *VSP) GetInfo(ctx context.Context) (*VspInfoResponse, error) {
	var response VspInfoResponse
	err := v.vspClient.get(ctx, apiVSPInfo, &response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

func (v *VSP) tx(hash *chainhash.Hash) (*wire.MsgTx, error) {
	txs, _, err := v.w.Internal().GetTransactionsByHashes(v.ctx, []*chainhash.Hash{hash})
	if err != nil {
		return nil, err
	}
	return txs[0], nil
}

func (v *VSP) GetFeeAddress(ctx context.Context, ticketHash chainhash.Hash) (dcrutil.Amount, error) {
	// Fetch ticket
	txs, _, err := v.w.Internal().GetTransactionsByHashes(ctx, []*chainhash.Hash{&ticketHash})
	if err != nil {
		return 0, errors.Errorf("failed to retrieve ticket %v: %v", ticketHash, err)
	}
	ticketTx := txs[0]

	// Fetch parent transaction
	parentHash := ticketTx.TxIn[0].PreviousOutPoint.Hash
	const scriptVersion = 0
	_, addrs := stdscript.ExtractAddrs(scriptVersion, ticketTx.TxOut[0].PkScript, v.w.Internal().ChainParams())
	if err != nil {
		return 0, errors.Errorf("failed to extract stake submission address from %v: %v", ticketHash, err)
	}
	if len(addrs) == 0 {
		return 0, errors.Errorf("failed to get address from %v", ticketHash)
	}
	votingAddress := addrs[0]

	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, v.chainParams)
	if err != nil {
		return 0, errors.Errorf("failed to extract script addr from %v: %v", ticketHash, err)
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
	err = v.vspClient.post(ctx, "/api/v3/feeaddress", commitmentAddr, &feeResponse,
		json.RawMessage(requestBody))
	if err != nil {
		return 0, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, feeResponse.Request) {
		return 0, fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?

	feeAddress, err := stdaddr.DecodeAddress(feeResponse.FeeAddress, v.w.Internal().ChainParams())
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

type valueOut struct {
	Remember string
	List     []string
}

func (mw *MultiWallet) GetVSPList(net string) (*VSPList, error) {
	var valueOut valueOut

	mw.ReadUserConfigValue(VSPHostConfigKey, &valueOut)
	var loadedVSP []*VSPInfo

	for _, host := range valueOut.List {
		info, err := mw.getInfo(host)
		if err != nil {
			return nil, err
		}

		loadedVSP = append(loadedVSP, &VSPInfo{
			Host: host,
			Info: info,
		})
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

	return &VSPList{
		List: loadedVSP,
	}, nil
}

func (mw *MultiWallet) AddVSP(host, net string) error {
	var valueOut valueOut

	// check if host already exists
	_ = mw.ReadUserConfigValue(VSPHostConfigKey, &valueOut)
	for _, v := range valueOut.List {
		if v == host {
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
	// (*wl.VspInfo).List = append((*wl.VspInfo).List, &wallet.VSPInfo{
	// 	Host: host,
	// 	Info: info,
	// })
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
	vsp.vspClient = newVSPClient(host, nil, sourceWallet.Internal())
	vspInfo, err := vsp.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	return vspInfo, nil
}

func (mw *MultiWallet) SetAutoTicketsBuyerConfig(vspHost string, walletID int, purchaseAccount int32, amountToMaintain int64) {
	mw.SetLongConfigValueForKey(TicketBuyerATMConfigKey, amountToMaintain)
	mw.SetIntConfigValueForKey(TicketBuyerWalletConfigKey, walletID)
	mw.SetInt32ConfigValueForKey(TicketBuyerAccountConfigKey, purchaseAccount)
	mw.SetStringConfigValueForKey(TicketBuyerVSPHostConfigKey, vspHost)
	mw.SetBoolConfigValueForKey(TicketBuyerConfigSet, true)
}

func (mw *MultiWallet) GetAutoTicketsBuyerConfig() (vspHost string, walletID int, purchaseAccount int32, amountToMaintain int64) {
	atm := mw.ReadLongConfigValueForKey(TicketBuyerATMConfigKey, -1)
	walId := mw.ReadIntConfigValueForKey(TicketBuyerWalletConfigKey, -1)
	accNum := mw.ReadInt32ConfigValueForKey(TicketBuyerAccountConfigKey, -1)
	vspHost = mw.ReadStringConfigValueForKey(TicketBuyerVSPHostConfigKey)
	return vspHost, walId, accNum, atm
}

func (mw *MultiWallet) TicketBuyerConfigIsSet() bool {
	return mw.ReadBoolConfigValueForKey(TicketBuyerConfigSet, false)
}

func (mw *MultiWallet) ClearTicketBuyerConfig() {
	mw.SetLongConfigValueForKey(TicketBuyerATMConfigKey, -1)
	mw.SetIntConfigValueForKey(TicketBuyerWalletConfigKey, -1)
	mw.SetInt32ConfigValueForKey(TicketBuyerAccountConfigKey, -1)
	mw.SetStringConfigValueForKey(TicketBuyerVSPHostConfigKey, "")
	mw.SetBoolConfigValueForKey(TicketBuyerConfigSet, false)
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
