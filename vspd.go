package dcrlibwallet

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
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
)

type VSPD struct {
	baseURL             string
	pubKey              []byte
	sourceWallet        *Wallet
	mwRef               *MultiWallet
	sourceAccountNumber int32
	httpClient          *http.Client
	params              *chaincfg.Params
}

func (mw *MultiWallet) NewVSPD(baseURL string, walletID int, sourceAccountNumber int32) *VSPD {
	sourceWallet := mw.WalletWithID(walletID)
	if sourceWallet == nil {
		return nil
	}

	return &VSPD{
		baseURL:             baseURL,
		sourceWallet:        sourceWallet,
		mwRef:               mw,
		sourceAccountNumber: sourceAccountNumber,
		httpClient:          new(http.Client),
		params:              mw.chainParams,
	}
}

// GetInfo returns the information of the specified VSP base URL
func (v *VSPD) GetInfo() (*GetVspInfoResponse, error) {
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

// GetVSPFeeAddress is the first part of submiting ticket to a VSP. It returns a
// fee address and an amount that must be paid. The fee Tx details must be sent
// in the PayFee method for the submittion to be recorded
func (v *VSPD) GetVSPFeeAddress(ticketHash string, passphrase []byte) (*GetFeeAddressResponse, error) {
	if ticketHash == "" {
		return nil, errors.New("no ticketHash provided")
	}

	txs, commitmentAddr, err := v.getTxAndAddress(ticketHash)
	if err != nil {
		return nil, err
	}

	txBuf := new(bytes.Buffer)
	txBuf.Grow(txs.SerializeSize())
	err = txs.Serialize(txBuf)
	if err != nil {
		log.Errorf("failed to serialize ticket %v: %v", ticketHash, err)
		return nil, err
	}

	req := GetFeeAddressRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash,
		TicketHex:  hex.EncodeToString(txBuf.Bytes()),
	}

	resp, err := v.signedVSP_HTTP("/api/v3/feeaddress", http.MethodPost, commitmentAddr.String(), passphrase, req)
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

// CreateTicketTeeTx gets fee info from GetVSPFeeAddress makes payment and returns tx hash for PayVSPFee
// ticket verification
// Before calling this function kindly ensure you are connected to the decred network by calling SpvSync().
func (v *VSPD) CreateTicketTeeTx(feeAmount int64, feeAddress string, passphrase []byte) ([]byte, string, error) {
	if feeAmount == 0 {
		return nil, "", errors.New("no feeAmount provided")
	}

	if feeAddress == "" {
		return nil, "", errors.New("no feeAddress provided")
	}

	txAuthor := v.mwRef.NewUnsignedTx(v.sourceWallet, v.sourceAccountNumber)
	txAuthor.AddSendDestination(feeAddress, feeAmount, false)
	feeTxHash, err := txAuthor.Broadcast(passphrase)
	if err != nil {
		return nil, "", err
	}

	feeTxString := hex.EncodeToString(feeTxHash)
	return feeTxHash, feeTxString, nil
}

// PayVSPFee is the second part of submitting ticket to a VSP. The fee amount is
// gotten from GetVSPFeeAddress
func (v *VSPD) PayVSPFee(feeTx, ticketHash, feeAddress string, passphrase []byte) (*PayFeeResponse, error) {
	if ticketHash == "" {
		return nil, errors.New("no ticketHash provided")
	}

	if feeTx == "" {
		return nil, errors.New("no feeTx provided")
	}

	txs, commitmentAddr, err := v.getTxAndAddress(ticketHash)
	if err != nil {
		return nil, err
	}

	_, votingAddress, _, err := txscript.ExtractPkScriptAddrs(0, txs.TxOut[0].PkScript, v.params)
	if err != nil {
		log.Warnf("failed to get voting Address: %v", err)
		return nil, err
	}

	if len(votingAddress) < 0 {
		return nil, errors.New("votingAddress is not greater 0")
	}

	err = v.sourceWallet.UnlockWallet(passphrase)
	if err != nil {
		return nil, err
	}

	votingKey, err := v.sourceWallet.internal.DumpWIFPrivateKey(v.sourceWallet.shutdownContext(), votingAddress[0])
	if err != nil {
		log.Warnf("failed to get votingKeyWIF for %v: %v", votingAddress[0], err)
		return nil, err
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

	resp, err := v.signedVSP_HTTP("/api/v3/payfee", http.MethodPost, commitmentAddr.String(), passphrase, req)
	if err != nil {
		return nil, err
	}

	var payFeeResponse PayFeeResponse
	err = json.Unmarshal(resp, &payFeeResponse)
	if err != nil {
		return nil, err
	}

	return &payFeeResponse, nil
}

// GetTicketStatus returns the status of the specified ticket from the VSP
func (v *VSPD) GetTicketStatus(ticketHash string, passphrase []byte) (*TicketStatusResponse, error) {
	if ticketHash == "" {
		return nil, errors.New("no ticketHash provided")
	}

	_, commitmentAddr, err := v.getTxAndAddress(ticketHash)
	if err != nil {
		return nil, err
	}

	req := TicketStatusRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash,
	}

	resp, err := v.signedVSP_HTTP("/api/v3/ticketstatus", http.MethodPost, commitmentAddr.String(), passphrase, req)
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
func (v *VSPD) SetVoteChoices(ticketHash string, passphrase []byte, choices map[string]string) (*SetVoteChoicesResponse, error) {
	if ticketHash == "" {
		return nil, fmt.Errorf("no ticketHash provided")
	}

	_, commitmentAddr, err := v.getTxAndAddress(ticketHash)
	if err != nil {
		return nil, err
	}

	req := SetVoteChoicesRequest{
		Timestamp:   time.Now().Unix(),
		TicketHash:  ticketHash,
		VoteChoices: choices,
	}

	resp, err := v.signedVSP_HTTP("/api/v3/setvotechoices", http.MethodPost, commitmentAddr.String(), passphrase, req)
	if err != nil {
		return nil, err
	}

	var setVoteChoicesResponse SetVoteChoicesResponse
	err = json.Unmarshal(resp, &setVoteChoicesResponse)
	if err != nil {
		return nil, err
	}
	return &setVoteChoicesResponse, nil
}

// signedVSP_HTTP makes a request against a VSP API. The request will be JSON
// encoded and signed using the provided commitment address. The signature of
// the response is also validated using the VSP pubkey.
func (v *VSPD) signedVSP_HTTP(url, method, commitmentAddr string, passphrase []byte, request interface{}) ([]byte, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	url = strings.TrimSuffix(v.baseURL, "/") + url
	ctx := v.sourceWallet.shutdownContext()
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, err
	}

	signature, err := v.sourceWallet.SignMessage(passphrase, commitmentAddr, string(reqBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Add("VSP-Client-Signature", base64.StdEncoding.EncodeToString(signature))

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

func (v *VSPD) getTxAndAddress(ticketHash string) (*wire.MsgTx, dcrutil.Address, error) {
	hash, err := chainhash.NewHashFromStr(ticketHash)
	if err != nil {
		log.Errorf("failed to retrieve hash from %s: %v", ticketHash, err)
		return nil, nil, err
	}

	ctx := v.sourceWallet.shutdownContext()
	txs, _, err := v.sourceWallet.internal.GetTransactionsByHashes(ctx, []*chainhash.Hash{hash})
	if err != nil {
		log.Errorf("failed to retrieve transaction for %v: %v", hash, err)
		return nil, nil, err
	}

	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(txs[0].TxOut[1].PkScript, v.params)
	if err != nil {
		log.Errorf("failed to extract script addr from %v: %v", ticketHash, err)
		return nil, nil, err
	}

	return txs[0], commitmentAddr, nil
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
