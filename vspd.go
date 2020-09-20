package dcrlibwallet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
	"github.com/planetdecred/dcrlibwallet/addresshelper"
	"github.com/planetdecred/dcrlibwallet/txhelper"
)

type VSPD struct {
	baseURL             string
	pubKey              []byte
	sourceWallet        *Wallet
	mwRef               *MultiWallet
	sourceAccountNumber int32
	httpClient          http.Client
	params              *chaincfg.Params
}

func (mw *MultiWallet) NewVSPD(baseURL string, sourceWallet *Wallet, sourceAccountNumber int32,
	httpClient http.Client, params *chaincfg.Params) *VSPD {
	return &VSPD{
		baseURL:             baseURL,
		sourceWallet:        sourceWallet,
		mwRef:               mw,
		sourceAccountNumber: sourceAccountNumber,
		httpClient:          httpClient,
		params:              params,
	}
}

// GetInfo returns the information of the specified VSP base URL
func (v *VSPD) GetInfo(ctx context.Context) (*GetVspInfoResponse, error) {
	resp, err := v.httpClient.Get(v.baseURL + "/api/v3/vspinfo")
	if err != nil {
		return nil, err
	}

	b, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Non 200 response from server: %v", string(b))
	}

	var vspInfoResponse GetVspInfoResponse
	err = json.Unmarshal(b, &vspInfoResponse)
	if err != nil {
		return nil, err
	}
	v.pubKey = vspInfoResponse.PubKey

	err = validateVSPServerSignature(resp, v.pubKey, b)
	if err != nil {
		return nil, err
	}

	return &vspInfoResponse, nil
}

// SubmitTicket gets fee info from GetVSPFeeAddress makes payment and sends data to PayVSPFee
// for ticket verification
func (v *VSPD) SubmitTicket(ctx context.Context, ticketHash string, passphrase []byte) error {
	feeInfo, err := v.GetVSPFeeAddress(ctx, ticketHash, passphrase)
	if err != nil {
		log.Errorf("cannot get fee address, %s", err.Error())
		return err
	}

	txAuthor := v.mwRef.NewUnsignedTx(v.sourceWallet, v.sourceAccountNumber)
	txAuthor.AddSendDestination(feeInfo.FeeAddress, feeInfo.FeeAmount, true)
	feeTx, err := txAuthor.Broadcast(passphrase)
	if err != nil {
		return err
	}

	pkScript, err := addresshelper.PkScript(feeInfo.FeeAddress, v.sourceWallet.chainParams)
	// pkScript, err := txscript.PayToAddrScript(feeInfo.FeeAddress)
	if err != nil {
		log.Warnf("failed to generate pay to addr script for %v: %v", feeInfo.FeeAddress, err)
		return err
	}

	commitmentAddr, err := v.getcommitmentAddr(ticketHash, pkScript)
	if err != nil {
		return err
	}

	// submissionAddr, err := addresshelper.PkScriptAddresses(v.params, pkScript)
	_, submissionAddr, _, err := txscript.ExtractPkScriptAddrs(0, pkScript, v.params)
	if err != nil {
		log.Warnf("failed to get submission addr: %v", err)
		return err
	}

	if len(submissionAddr) > 0 {
		return errors.New("submissionAddr is not greater 0")
	}

	votingKey, err := v.sourceWallet.internal.DumpWIFPrivateKey(ctx, submissionAddr[0])
	if err != nil {
		log.Warnf("failed to get votingKeyWIF: %v", err)
		return err
	}

	if err = v.PayVSPFee(ctx, string(feeTx), votingKey, ticketHash, commitmentAddr.Address(), passphrase); err != nil {
		return err
	}
	return nil
}

// GetVSPFeeAddress is the first part of submiting ticket to a VSP. It returns a
// fee address and an amount that must be paid. The fee Tx details must be sent
// in the PayFee method for the submittion to be recorded
func (v *VSPD) GetVSPFeeAddress(ctx context.Context, ticketHash string, passphrase []byte) (*GetFeeAddressResponse, error) {
	if ticketHash == "" {
		return nil, fmt.Errorf("no ticketHash provided")
	}

	txs, msgTx, err := v.getMsgTx(ticketHash)
	if err != nil {
		return nil, err
	}

	commitmentAddr, err := v.getcommitmentAddr(ticketHash, msgTx.TxOut[0].PkScript)
	if err != nil {
		return nil, err
	}

	req := GetFeeAddressRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash,
		TicketHex:  txs.Hex,
	}

	resp, err := v.signedVSP_HTTP(ctx, "/api/v3/feeaddress", http.MethodPost, commitmentAddr.String(), passphrase, req)
	if err != nil {
		return nil, err
	}

	var feeAddressResponse GetFeeAddressResponse
	err = json.Unmarshal(resp, &feeAddressResponse)
	if err != nil {
		return nil, err
	}
	return &feeAddressResponse, nil
}

// PayVSPFee is the second part of submitting ticket to a VSP. The fee amount is
// gotten from GetVSPFeeAddress
func (v *VSPD) PayVSPFee(ctx context.Context, feeTx, votingKey, ticketHash, commitmentAddr string, passphrase []byte) error {
	if ticketHash == "" {
		return fmt.Errorf("no ticketHash provided")
	}

	voteChoices := make(map[string]string)
	voteChoices[chaincfg.VoteIDHeaderCommitments] = "yes"

	req := PayFeeRequest{
		FeeTx:       feeTx,
		VotingKey:   votingKey,
		TicketHash:  ticketHash,
		Timestamp:   time.Now().Unix(),
		VoteChoices: voteChoices,
	}

	_, err := v.signedVSP_HTTP(ctx, "/api/v3/payfee", http.MethodPost, commitmentAddr, passphrase, req)
	if err != nil {
		return err
	}

	log.Infof("successfully processed %v", ticketHash)
	return nil
}

// GetTicketStatus returns the status of the specified ticket from the VSP
func (v *VSPD) GetTicketStatus(ctx context.Context, ticketHash string, passphrase []byte) (*TicketStatusResponse, error) {
	_, msgTx, err := v.getMsgTx(ticketHash)
	if err != nil {
		return nil, err
	}

	commitmentAddr, err := v.getcommitmentAddr(ticketHash, msgTx.TxOut[0].PkScript)
	if err != nil {
		return nil, err
	}

	req := TicketStatusRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash,
	}

	resp, err := v.signedVSP_HTTP(ctx, "/api/v3/ticketstatus", http.MethodPost, commitmentAddr.String(), passphrase, req)
	if err != nil {
		return nil, err
	}

	var ticketStatusResponse TicketStatusResponse
	err = json.Unmarshal(resp, &ticketStatusResponse)
	if err != nil {
		return nil, err
	}
	return &ticketStatusResponse, nil
}

// SetVoteChoices updates the vote choice of the specified ticket on the VSP
func (v *VSPD) SetVoteChoices(ctx context.Context, ticketHash string, passphrase []byte, choices map[string]string) error {
	if ticketHash == "" {
		return fmt.Errorf("no ticketHash provided")
	}

	_, msgTx, err := v.getMsgTx(ticketHash)
	if err != nil {
		return err
	}

	commitmentAddr, err := v.getcommitmentAddr(ticketHash, msgTx.TxOut[1].PkScript)
	if err != nil {
		return err
	}

	req := SetVoteChoicesRequest{
		Timestamp:   time.Now().Unix(),
		TicketHash:  ticketHash,
		VoteChoices: choices,
	}

	_, err = v.signedVSP_HTTP(ctx, "/api/v3/setvotechoices", http.MethodPost, commitmentAddr.String(), passphrase, req)
	if err != nil {
		return err
	}

	return nil
}

// signedVSP_HTTP makes a request against a VSP API. The request will be JSON
// encoded and signed using the provided commitment address. The signature of
// the response is also validated using the VSP pubkey.
func (v *VSPD) signedVSP_HTTP(ctx context.Context, url, method, commitmentAddr string, passphrase []byte,
	request interface{}) ([]byte, error) {

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	url = strings.TrimSuffix(v.baseURL, "/") + url
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, err
	}

	signature, err := v.sourceWallet.SignMessage(passphrase, commitmentAddr, string(reqBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Add("VSP-Client-Signature", hex.EncodeToString(signature))

	var httpClient http.Client
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	b, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Non 200 response from server: %v", string(b))
	}

	err = validateVSPServerSignature(resp, v.pubKey, b)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (v *VSPD) getcommitmentAddr(ticketHash string, pkScript []byte) (dcrutil.Address, error) {
	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(pkScript, v.params)
	if err != nil {
		log.Errorf("failed to extract script addr from %v: %v", ticketHash, err)
		return nil, err
	}

	return commitmentAddr, nil
}

func (v *VSPD) getMsgTx(ticketHash string) (*Transaction, *wire.MsgTx, error) {
	hash, err := chainhash.NewHashFromStr(ticketHash)
	if err != nil {
		log.Errorf("failed to retrieve hash from %s: %v", ticketHash, err)
		return nil, nil, err
	}

	txs, err := v.sourceWallet.GetTransactionRaw(hash[:])
	if err != nil {
		log.Errorf("failed to retrieve transaction for %v: %v", hash, err)
		return nil, nil, err
	}

	msgTx, _, _, _, err := txhelper.MsgTxFeeSizeRate(txs.Hex)
	if err != nil {
		return nil, nil, err
	}

	return txs, msgTx, nil
}

func validateVSPServerSignature(resp *http.Response, pubKey, body []byte) error {
	sigStr := resp.Header.Get("VSP-Server-Signature")
	sig, err := hex.DecodeString(sigStr)
	if err != nil {
		return fmt.Errorf("Error validating VSP signature: %v", err)
	}

	if !ed25519.Verify(pubKey, body, sig) {
		return errors.New("Bad signature from VSP")
	}

	return nil
}
