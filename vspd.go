package dcrlibwallet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"decred.org/dcrwallet/errors"

	"github.com/decred/dcrwallet/wallet/v3"

	w "decred.org/dcrwallet/wallet"
	"decred.org/dcrwallet/wallet/txauthor"
	"decred.org/dcrwallet/wallet/txsizes"
	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

const (
	apiVSPInfo    = "/api/v3/vspinfo"
	requiredConfs = 6 + 2
)

type VSPD struct {
	vspHost         string
	pubKey          []byte
	w               *Wallet
	mwRef           *MultiWallet
	purchaseAccount uint32
	changeAccount   uint32
	chainParams     *chaincfg.Params

	*client
	ctx context.Context
	c   *w.ConfirmationNotificationsClient

	ticketToFeeLock sync.Mutex
	feesToTickets   map[chainhash.Hash]chainhash.Hash
	ticketsToFees   map[chainhash.Hash]PendingFee
}

func (mw *MultiWallet) NewVSPD(vspHost string, walletID int, purchaseAccount uint32) (*VSPD, error) {
	sourceWallet := mw.WalletWithID(walletID)
	if sourceWallet == nil {
		return nil, fmt.Errorf("wallet doesn't exist")
	}

	v := &VSPD{
		vspHost:         vspHost,
		w:               sourceWallet,
		mwRef:           mw,
		purchaseAccount: purchaseAccount,
		changeAccount:   purchaseAccount,
		chainParams:     mw.chainParams,
	}

	ctx, cancel := mw.contextWithShutdownCancel()
	mw.cancelFuncs = append(mw.cancelFuncs, cancel)
	vspInfo, err := v.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	v.ctx = ctx
	v.c = sourceWallet.internal.NtfnServer.ConfirmationNotifications(ctx)
	v.client = newClient(vspHost, vspInfo.PubKey, sourceWallet.internal)
	return v, nil
}

func (v *VSPD) PoolFee(ctx context.Context) (float64, error) {
	var vspInfo VspInfoResponse
	err := v.client.get(ctx, apiVSPInfo, &vspInfo)
	if err != nil {
		return -1, err
	}
	return vspInfo.FeePercentage, nil
}

// PurchaseStakeTickets purchases the number of tickets passed as an argument and pays the fees for the tickets
func (v *VSPD) PurchaseStakeTickets(tickets, expiryBlocks int32, passphrase []byte) error {
	defer v.w.LockWallet()
	passphraseCopy := make([]byte, len(passphrase))
	_ = copy(passphraseCopy, passphrase)

	err := v.w.UnlockWallet(passphraseCopy)
	if err != nil {
		return translateError(err)
	}

	request := &PurchaseTicketsRequest{
		Account:               v.purchaseAccount,
		Passphrase:            passphrase,
		NumTickets:            uint32(tickets),
		Expiry:                uint32(v.w.GetBestBlock() + expiryBlocks),
		RequiredConfirmations: DefaultRequiredConfirmations,
	}

	request.VSPFeeProcess = v.PoolFee
	request.VSPFeePaymentProcess = v.ProcessFee

	_, err = v.w.purchaseTickets(v.ctx, request, v.vspHost)
	if err != nil {
		return err
	}

	return nil
}

// ProcessFee
func (v *VSPD) ProcessFee(ctx context.Context, ticketHash chainhash.Hash, credits []w.Input) (*wire.MsgTx, error) {
	var feeAmount dcrutil.Amount
	var err error
	for i := 0; i < 2; i++ {
		feeAmount, err = v.GetFeeAddress(ctx, ticketHash)
		if err == nil {
			break
		}
		const broadcastMsg = "ticket transaction could not be broadcast"
		if err != nil && i == 0 && strings.Contains(err.Error(), broadcastMsg) {
			time.Sleep(2 * time.Minute)
		}
	}
	if err != nil {
		return nil, err
	}

	var totalValue int64
	if credits == nil {
		const minconf = 1
		// PurchaseAccount => sourceAccount
		credits, err = v.w.internal.ReserveOutputsForAmount(ctx, v.purchaseAccount, feeAmount, minconf)
		if err != nil {
			return nil, err
		}
		for _, credit := range credits {
			totalValue += credit.PrevOut.Value
		}
		if dcrutil.Amount(totalValue) < feeAmount {
			return nil, fmt.Errorf("reserved credits insufficient: %v < %v",
				dcrutil.Amount(totalValue), feeAmount)
		}
	}

	feeTx, err := v.CreateFeeTx(ctx, ticketHash, credits)
	if err != nil {
		return nil, err
	}
	// set fee tx as unpublished, because it will be published by the vsp.
	feeHash := feeTx.TxHash()
	err = v.w.internal.AddTransaction(ctx, feeTx, nil)
	if err != nil {
		return nil, err
	}
	err = v.w.internal.SetPublished(ctx, &feeHash, false)
	if err != nil {
		return nil, err
	}
	paidTx, err := v.PayFee(ctx, ticketHash, feeTx)
	if err != nil {
		return nil, err
	}
	err = v.w.internal.UpdateVspTicketFeeToPaid(ctx, &ticketHash, &feeHash)
	if err != nil {
		return nil, err
	}
	return paidTx, nil
}

type changeSource struct {
	script  []byte
	version uint16
}

func (c changeSource) Script() ([]byte, uint16, error) {
	return c.script, c.version, nil
}

func (c changeSource) ScriptSize() int {
	return len(c.script)
}

func (v *VSPD) CreateFeeTx(ctx context.Context, ticketHash chainhash.Hash, credits []w.Input) (*wire.MsgTx, error) {
	if len(credits) == 0 {
		return nil, fmt.Errorf("no credits passed")
	}

	v.ticketToFeeLock.Lock()
	feeInfo, exists := v.ticketsToFees[ticketHash]
	v.ticketToFeeLock.Unlock()
	if !exists {
		_, err := v.GetFeeAddress(ctx, ticketHash)
		if err != nil {
			return nil, err
		}
		v.ticketToFeeLock.Lock()
		feeInfo, exists = v.ticketsToFees[ticketHash]
		v.ticketToFeeLock.Unlock()
		if !exists {
			return nil, fmt.Errorf("failed to find fee info for ticket %v", ticketHash)
		}
	}

	// validate credits
	var totalValue int64
	for _, credit := range credits {
		totalValue += credit.PrevOut.Value
	}
	if dcrutil.Amount(totalValue) < feeInfo.FeeAmount {
		return nil, fmt.Errorf("not enough fee: %v < %v", dcrutil.Amount(totalValue), feeInfo.FeeAmount)
	}

	pkScript, err := txscript.PayToAddrScript(feeInfo.FeeAddress)
	if err != nil {
		log.Warnf("failed to generate pay to addr script for %v: %v", feeInfo.FeeAddress, err)
		return nil, err
	}

	a, err := v.w.internal.NewChangeAddress(ctx, v.changeAccount)
	if err != nil {
		log.Warnf("failed to get new change address: %v", err)
		return nil, err
	}

	c, ok := a.(w.Address)
	if !ok {
		log.Warnf("failed to convert '%T' to wallet.Address", a)
		return nil, fmt.Errorf("failed to convert '%T' to wallet.Address", a)
	}

	cver, cscript := c.PaymentScript()
	cs := changeSource{
		script:  cscript,
		version: cver,
	}

	var inputSource txauthor.InputSource
	if len(credits) > 0 {
		inputSource = func(amount dcrutil.Amount) (*txauthor.InputDetail, error) {
			if amount < 0 {
				return nil, fmt.Errorf("invalid amount: %d < 0", amount)
			}

			var detail txauthor.InputDetail
			if amount == 0 {
				return &detail, nil
			}
			for _, credit := range credits {
				if detail.Amount >= amount {
					break
				}

				log.Infof("credit: %v %v", credit.OutPoint.String(), dcrutil.Amount(credit.PrevOut.Value))

				// TODO: copied from txauthor.MakeInputSource - make a shared function?
				// Unspent credits are currently expected to be either P2PKH or
				// P2PK, P2PKH/P2SH nested in a revocation/stakechange/vote output.
				var scriptSize int
				scriptClass := txscript.GetScriptClass(0, credit.PrevOut.PkScript, true) // Yes treasury
				switch scriptClass {
				case txscript.PubKeyHashTy:
					scriptSize = txsizes.RedeemP2PKHSigScriptSize
				case txscript.PubKeyTy:
					scriptSize = txsizes.RedeemP2PKSigScriptSize
				case txscript.StakeRevocationTy, txscript.StakeSubChangeTy,
					txscript.StakeGenTy:
					scriptClass, err = txscript.GetStakeOutSubclass(credit.PrevOut.PkScript, true) // Yes treasury
					if err != nil {
						return nil, fmt.Errorf(
							"failed to extract nested script in stake output: %v",
							err)
					}

					// For stake transactions we expect P2PKH and P2SH script class
					// types only but ignore P2SH script type since it can pay
					// to any script which the wallet may not recognize.
					if scriptClass != txscript.PubKeyHashTy {
						log.Errorf("unexpected nested script class for credit: %v",
							scriptClass)
						continue
					}

					scriptSize = txsizes.RedeemP2PKHSigScriptSize
				default:
					log.Errorf("unexpected script class for credit: %v",
						scriptClass)
					continue
				}

				inputs := wire.NewTxIn(&credit.OutPoint, credit.PrevOut.Value, credit.PrevOut.PkScript)

				detail.Amount += dcrutil.Amount(credit.PrevOut.Value)
				detail.Inputs = append(detail.Inputs, inputs)
				detail.Scripts = append(detail.Scripts, credit.PrevOut.PkScript)
				detail.RedeemScriptSizes = append(detail.RedeemScriptSizes, scriptSize)
			}
			return &detail, nil
		}
	}

	txOut := []*wire.TxOut{wire.NewTxOut(int64(feeInfo.FeeAmount), pkScript)}
	feeTx, err := v.w.internal.NewUnsignedTransaction(ctx, txOut, v.w.internal.RelayFee(), v.purchaseAccount, 6,
		wallet.OutputSelectionAlgorithmDefault, cs, inputSource)
	if err != nil {
		log.Warnf("failed to create fee transaction: %v", err)
		return nil, err
	}
	if feeTx.ChangeIndex >= 0 {
		feeTx.RandomizeChangePosition()
	}

	sigErrs, err := v.w.internal.SignTransaction(ctx, feeTx.Tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil {
		log.Errorf("failed to sign transaction: %v", err)
		for _, sigErr := range sigErrs {
			log.Errorf("\t%v", sigErr)
		}
		return nil, err
	}

	return feeTx.Tx, nil
}

// PayFee receives an unsigned fee tx, signs it and make a pays fee request to
// the vsp, so the ticket get registered.
func (v *VSPD) PayFee(ctx context.Context, ticketHash chainhash.Hash, feeTx *wire.MsgTx) (*wire.MsgTx, error) {
	if feeTx == nil {
		return nil, fmt.Errorf("nil fee tx")
	}

	txBuf := new(bytes.Buffer)
	txBuf.Grow(feeTx.SerializeSize())
	err := feeTx.Serialize(txBuf)
	if err != nil {
		log.Errorf("failed to serialize fee transaction: %v", err)
		return nil, err
	}

	v.ticketToFeeLock.Lock()
	feeInfo, exists := v.ticketsToFees[ticketHash]
	v.ticketToFeeLock.Unlock()
	if !exists {
		return nil, fmt.Errorf("call GetFeeAddress first")
	}

	votingKeyWIF, err := v.w.internal.DumpWIFPrivateKey(ctx, feeInfo.VotingAddress)
	if err != nil {
		log.Errorf("failed to retrieve privkey for %v: %v", feeInfo.VotingAddress, err)
		return nil, err
	}

	// Retrieve voting preferences
	agendaChoices, _, err := v.w.internal.AgendaChoices(ctx, &ticketHash)
	if err != nil {
		log.Errorf("failed to retrieve agenda choices for %v: %v", ticketHash, err)
		return nil, err
	}

	voteChoices := make(map[string]string)
	for _, agendaChoice := range agendaChoices {
		voteChoices[agendaChoice.AgendaID] = agendaChoice.ChoiceID
	}

	var payfeeResponse PayFeeResponse
	requestBody, err := json.Marshal(&PayFeeRequest{
		Timestamp:   time.Now().Unix(),
		TicketHash:  ticketHash.String(),
		FeeTx:       hex.EncodeToString(txBuf.Bytes()),
		VotingKey:   votingKeyWIF,
		VoteChoices: voteChoices,
	})
	if err != nil {
		return nil, err
	}
	err = v.client.post(ctx, "/api/v3/payfee", feeInfo.CommitmentAddress,
		&payfeeResponse, json.RawMessage(requestBody))
	if err != nil {
		return nil, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, payfeeResponse.Request) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, payfeeResponse.Request)
		return nil, fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?

	v.ticketToFeeLock.Lock()
	feeInfo.FeeTx = feeTx
	v.ticketsToFees[ticketHash] = feeInfo
	v.ticketToFeeLock.Unlock()
	v.c.Watch([]*chainhash.Hash{&ticketHash}, requiredConfs)

	log.Infof("successfully processed %v", ticketHash)

	return feeTx, nil
}

func (v *VSPD) GetFeeAddress(ctx context.Context, ticketHash chainhash.Hash) (dcrutil.Amount, error) {
	// Fetch ticket
	txs, _, err := v.w.internal.GetTransactionsByHashes(ctx, []*chainhash.Hash{&ticketHash})
	if err != nil {
		log.Errorf("failed to retrieve ticket %v: %v", ticketHash, err)
		return 0, err
	}
	ticketTx := txs[0]

	// Fetch parent transaction
	parentHash := ticketTx.TxIn[0].PreviousOutPoint.Hash
	parentTxs, _, err := v.w.internal.GetTransactionsByHashes(ctx, []*chainhash.Hash{&parentHash})
	if err != nil {
		log.Errorf("failed to retrieve parent %v of ticket %v: %v", parentHash, ticketHash, err)
		return 0, err
	}
	parentTx := parentTxs[0]

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

	// Serialize ticket
	txBuf := new(bytes.Buffer)
	txBuf.Grow(ticketTx.SerializeSize())
	err = ticketTx.Serialize(txBuf)
	if err != nil {
		log.Errorf("failed to serialize ticket %v: %v", ticketHash, err)
		return 0, err
	}
	ticketHex := hex.EncodeToString(txBuf.Bytes())

	// Serialize parent
	txBuf.Reset()
	txBuf.Grow(parentTx.SerializeSize())
	err = parentTx.Serialize(txBuf)
	if err != nil {
		log.Errorf("failed to serialize parent %v of ticket %v: %v", parentHash, ticketHash, err)
		return 0, err
	}
	parentHex := hex.EncodeToString(txBuf.Bytes())

	var feeResponse FeeAddressResponse
	requestBody, err := json.Marshal(&FeeAddressRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash.String(),
		TicketHex:  ticketHex,
		ParentHex:  parentHex,
	})
	if err != nil {
		return 0, err
	}
	err = v.client.post(ctx, "/api/v3/feeaddress", commitmentAddr, &feeResponse,
		json.RawMessage(requestBody))
	if err != nil {
		return 0, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, feeResponse.Request) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, feeResponse.Request)
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

// GetInfo returns the information of the specified VSP base URL
func (v *VSPD) GetInfo(ctx context.Context) (*VspInfoResponse, error) {
	var response VspInfoResponse
	err := v.client.get(ctx, apiVSPInfo, &response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

func validateVSPServerSignature(resp *http.Response, pubKey, body []byte) error {
	sigStr := resp.Header.Get("VSP-Server-Signature")
	sig, err := base64.StdEncoding.DecodeString(sigStr)
	if err != nil {
		return fmt.Errorf("error validating VSP signature: %v", err)
	}

	if !ed25519.Verify(pubKey, body, sig) {
		return errors.New("bad signature from VSP")
	}

	return nil
}
