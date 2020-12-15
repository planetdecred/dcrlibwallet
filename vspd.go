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
	"reflect"
	"strings"
	"time"

	w "decred.org/dcrwallet/wallet"
	"github.com/asdine/storm"
	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

type VSPD struct {
	baseURL             string
	pubKey              []byte
	w                   *Wallet
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
		w:                   sourceWallet,
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

	err = validateVSPServerSignature(resp, vspInfoResponse.PubKey, b)
	if err != nil {
		return nil, err
	}
	v.pubKey = vspInfoResponse.PubKey

	return &vspInfoResponse, nil
}

// GetVSPFeeAddress is the first part of submiting ticket to a VSP. It returns a
// fee address and an amount that must be paid by calling CreateTicketFeeTx.
func (v *VSPD) GetVSPFeeAddress(ticketHash string, passphrase []byte) (*GetFeeAddressResponse, error) {
	if ticketHash == "" {
		return nil, errors.New("no ticketHash provided")
	}

	txs, commitmentAddr, err := v.getTxAndAddress(ticketHash)
	if err != nil {
		return nil, err
	}

	txBuf, err := txs.Bytes()
	if err != nil {
		log.Errorf("failed to serialize ticket %v: %v", ticketHash, err)
		return nil, err
	}
	ticketHex := hex.EncodeToString(txBuf)

	parentHash := txs.TxIn[0].PreviousOutPoint.Hash
	parentTxs, err := v.getTxFromHash(parentHash.String())
	if err != nil {
		log.Errorf("failed to retrieve parent %v of ticket %v: %v", parentHash, ticketHash, err)
		return nil, err
	}
	parentTx := parentTxs[0]

	// Serialize parent
	parentTxBuf, err := parentTx.Bytes()
	if err != nil {
		log.Errorf("failed to serialize parent %v of ticket %v: %v", parentHash, ticketHash, err)
		return nil, err
	}
	parentHex := hex.EncodeToString(parentTxBuf)
	req := GetFeeAddressRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash,
		TicketHex:  ticketHex,
		ParentHex:  parentHex,
	}

	resp, err := v.signedVSP_HTTP("/api/v3/feeaddress", commitmentAddr.String(), passphrase, req)
	if err != nil {
		return nil, err
	}

	var feeAddressResponse GetFeeAddressResponse
	err = json.Unmarshal(resp, &feeAddressResponse)
	if err != nil {
		return nil, err
	}

	err = verifyResponse(feeAddressResponse.Request, req)
	if err != nil {
		return nil, err
	}

	data := &VspdTicketInfo{
		Timestamp:  feeAddressResponse.Timestamp,
		Hash:       feeAddressResponse.Request.TicketHash,
		FeeAddress: feeAddressResponse.FeeAddress,
		FeeAmount:  feeAddressResponse.FeeAmount,
		Expiration: feeAddressResponse.Expiration,
	}

	err = v.updateVspdDBRecord(data, ticketHash)
	if err != nil {
		return nil, err
	}

	return &feeAddressResponse, nil
}

// CreateTicketFeeTx gets fee info from GetVSPFeeAddress makes payment and returns tx hash for PayVSPFee
// ticket verification
func (v *VSPD) CreateTicketFeeTx(feeAmount int64, ticketHash, feeAddress string, passphrase []byte) (string, error) {
	if ticketHash == "" || feeAmount == 0 || feeAddress == "" {
		return "", errors.New("missing required parameters")
	}

	record, err := v.getVspdDBRecord(ticketHash, &VspdTicketInfo{})
	if err != nil {
		return "", err
	}

	feeTx := reflect.Indirect(*record).FieldByName("FeeTx").String()

	if feeTx != "" {
		log.Errorf("vspd ticket for %v has feeTx %v confirm fees by calling PayVSPFee()", ticketHash, feeTx)
		return "", errors.New("vspd ticket fee has been created. Use PayVSPFee() to register ticket")
	}

	txAuthor := v.mwRef.NewUnsignedTx(v.w, v.sourceAccountNumber)
	txAuthor.AddSendDestination(feeAddress, feeAmount, false)

	unsignedTx, err := txAuthor.constructTransaction()
	if err != nil {
		return "", translateError(err)
	}

	if unsignedTx.ChangeIndex >= 0 {
		unsignedTx.RandomizeChangePosition()
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{}
	}()

	ctx := txAuthor.sourceWallet.shutdownContext()
	err = txAuthor.sourceWallet.internal.Unlock(ctx, passphrase, lock)
	if err != nil {
		log.Error(err)
		return "", errors.New(ErrInvalidPassphrase)
	}

	invalidSigs, err := txAuthor.sourceWallet.internal.SignTransaction(ctx, unsignedTx.Tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil {
		log.Errorf("failed to sign transaction: %v", err)
		for _, sigErr := range invalidSigs {
			log.Errorf("\t%v", sigErr)
		}
		return "", err
	}

	txBuf := new(bytes.Buffer)
	txBuf.Grow(unsignedTx.Tx.SerializeSize())
	err = unsignedTx.Tx.Serialize(txBuf)
	if err != nil {
		log.Errorf("failed to serialize fee transaction: %v", err)
		return "", err
	}

	//update db with feetx data
	data := &VspdTicketInfo{
		Hash:  ticketHash,
		FeeTx: hex.EncodeToString(txBuf.Bytes()),
	}

	err = v.updateVspdDBRecord(data, ticketHash)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(txBuf.Bytes()), nil
}

// PayVSPFee is the second part of submitting ticket to a VSP. The feeTx is gotton from CreateTicketFeeTx
// and feeAddress from GetVSPFeeAddress
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

	_, votingAddress, _, err := txscript.ExtractPkScriptAddrs(0, txs.TxOut[0].PkScript, v.params, true)
	if err != nil {
		log.Warnf("failed to get voting Address: %v", err)
		return nil, err
	}

	if len(votingAddress) < 0 {
		return nil, errors.New("votingAddress is not greater 0")
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{}
	}()

	// unlock wallet
	ctx := v.w.shutdownContext()
	err = v.w.internal.Unlock(ctx, passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	votingKey, err := v.w.internal.DumpWIFPrivateKey(v.w.shutdownContext(), votingAddress[0])
	if err != nil {
		log.Warnf("failed to get votingKeyWIF for %v: %v", votingAddress[0], err)
		return nil, err
	}

	agendaChoices, err := v.getAgenda(ticketHash)
	if err != nil {
		return nil, err
	}

	voteChoices := make(map[string]string)
	for _, agendaChoice := range agendaChoices {
		voteChoices[agendaChoice.AgendaID] = agendaChoice.ChoiceID
	}

	req := PayFeeRequest{
		FeeTx:       feeTx,
		VotingKey:   votingKey,
		TicketHash:  ticketHash,
		Timestamp:   time.Now().Unix(),
		VoteChoices: voteChoices,
	}

	resp, err := v.signedVSP_HTTP("/api/v3/payfee", commitmentAddr.String(), passphrase, req)
	if err != nil {
		return nil, err
	}

	var payFeeResponse PayFeeResponse
	err = json.Unmarshal(resp, &payFeeResponse)
	if err != nil {
		return nil, err
	}

	err = verifyResponse(payFeeResponse.Request, req)
	if err != nil {
		return nil, err
	}

	data := &VspdTicketInfo{
		Timestamp:   payFeeResponse.Timestamp,
		Hash:        payFeeResponse.Request.TicketHash,
		FeeTx:       payFeeResponse.Request.FeeTx,
		VoteChoices: payFeeResponse.Request.VoteChoices,
	}

	err = v.updateVspdDBRecord(data, ticketHash)
	if err != nil {
		return nil, err
	}

	return &payFeeResponse, nil
}

// GetTicketStatus returns the status of the specified ticket from the VSP, after calling PayVSPFee
func (v *VSPD) GetTicketStatus(ticketHash string, passphrase []byte) (*TicketStatusResponse, error) {
	if ticketHash == "" {
		return nil, errors.New("no ticketHash provided")
	}

	_, commitmentAddr, err := v.getTxAndAddress(ticketHash)
	if err != nil {
		return nil, err
	}

	req := TicketStatusRequest{
		TicketHash: ticketHash,
	}

	resp, err := v.signedVSP_HTTP("/api/v3/ticketstatus", commitmentAddr.String(), passphrase, req)
	if err != nil {
		return nil, err
	}

	var ticketStatusResponse TicketStatusResponse
	err = json.Unmarshal(resp, &ticketStatusResponse)
	if err != nil {
		return nil, err
	}

	err = verifyResponse(ticketStatusResponse.Request, req)
	if err != nil {
		return nil, err
	}

	data := &VspdTicketInfo{
		Timestamp:       ticketStatusResponse.Timestamp,
		Hash:            ticketStatusResponse.Request.TicketHash,
		TicketConfirmed: ticketStatusResponse.TicketConfirmed,
		VoteChoices:     ticketStatusResponse.VoteChoices,
		FeeTxStatus:     ticketStatusResponse.FeeTxStatus,
		FeeTxHash:       ticketStatusResponse.FeeTxHash,
	}

	err = v.updateVspdDBRecord(data, ticketHash)
	if err != nil {
		return nil, err
	}

	return &ticketStatusResponse, nil
}

// SetVoteChoices updates the vote choice of the specified ticket on the VSP, after calling PayVSPFee
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

	resp, err := v.signedVSP_HTTP("/api/v3/setvotechoices", commitmentAddr.String(), passphrase, req)
	if err != nil {
		return nil, err
	}

	var setVoteChoicesResponse SetVoteChoicesResponse
	err = json.Unmarshal(resp, &setVoteChoicesResponse)
	if err != nil {
		return nil, err
	}

	err = verifyResponse(setVoteChoicesResponse.Request, req)
	if err != nil {
		return nil, err
	}

	data := &VspdTicketInfo{
		Timestamp:   setVoteChoicesResponse.Timestamp,
		Hash:        setVoteChoicesResponse.Request.TicketHash,
		VoteChoices: setVoteChoicesResponse.Request.VoteChoices,
	}

	err = v.updateVspdDBRecord(data, ticketHash)
	if err != nil {
		return nil, err
	}

	return &setVoteChoicesResponse, nil
}

// signedVSP_HTTP makes a request against a VSP API. The request will be JSON
// encoded and signed using the provided commitment address. The signature of
// the response is also validated using the VSP pubkey.
func (v *VSPD) signedVSP_HTTP(path, commitmentAddr string, passphrase []byte, request interface{}) ([]byte, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	path = strings.TrimSuffix(v.baseURL, "/") + path
	ctx := v.w.shutdownContext()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, err
	}

	signature, err := v.w.SignMessage(passphrase, commitmentAddr, string(reqBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Add("VSP-Client-Signature", base64.StdEncoding.EncodeToString(signature))

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	b, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vsp responded with an error: %v", string(b))
	}

	err = validateVSPServerSignature(resp, v.pubKey, b)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (v *VSPD) getTxAndAddress(ticketHash string) (*wire.MsgTx, dcrutil.Address, error) {
	txs, err := v.getTxFromHash(ticketHash)
	if err != nil {
		return nil, nil, err
	}

	if len(txs) == 0 {
		return nil, nil, fmt.Errorf("ticket has not found: %s", ticketHash)
	}

	tx := txs[0]
	ticketCommitmentOutput := tx.TxOut[1]
	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketCommitmentOutput.PkScript, v.params)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract script addr from %v: %v", ticketHash, err)
	}

	return tx, commitmentAddr, nil
}

// insert or update VSPD ticket info
func (v *VSPD) updateVspdDBRecord(data interface{}, ticketHash string) error {
	updated, err := v.w.walletDataDB.SaveOrUpdateVspdRecord(&VspdTicketInfo{}, data)
	if err != nil {
		fmt.Errorf("[%d] new vspd save err : %v", v.w.ID, err)
		return err
	}

	if !updated {
		log.Infof("[%d] New vspd ticket %s added", v.w.ID, ticketHash)
	} else {
		log.Infof("[%d] vspd ticket %s updated", v.w.ID, ticketHash)
	}

	return nil
}

// get VSPD ticket info
func (v *VSPD) getVspdDBRecord(txHash string, dataPointer interface{}) (*reflect.Value, error) {
	err := v.w.walletDataDB.FindOne("Hash", txHash, dataPointer)
	if err != nil {
		log.Errorf("failed to find vspd ticket for %v: %v", txHash, err)
		if err == storm.ErrNotFound {
			return nil, fmt.Errorf("vspd ticket: " + txHash + " " + err.Error() + ". ensure you first call GetVSPFeeAddress()")
		}
		return nil, err
	}

	val := reflect.ValueOf(dataPointer)
	return &val, nil
}

// get VSPD ticket info
func (v *VSPD) getAgenda(txHash string) ([]w.AgendaChoice, error) {
	hash, err := getHashFromStr(txHash)
	if err != nil {
		return nil, err
	}

	agendaChoices, _, err := v.w.internal.AgendaChoices(v.w.shutdownContext(), hash)
	if err != nil {
		log.Errorf("failed to retrieve agenda choices for %v: %v", txHash, err)
		return nil, err
	}

	return agendaChoices, nil
}

func (v *VSPD) getTxFromHash(txHash string) ([]*wire.MsgTx, error) {
	hash, err := getHashFromStr(txHash)
	if err != nil {
		return nil, err
	}

	txs, _, err := v.w.internal.GetTransactionsByHashes(v.w.shutdownContext(), []*chainhash.Hash{hash})
	if err != nil {
		log.Errorf("failed to retrieve transaction for %v: %v", hash, err)
		return nil, err
	}

	return txs, nil
}

func getHashFromStr(txHash string) (*chainhash.Hash, error) {
	hash, err := chainhash.NewHashFromStr(txHash)
	if err != nil {
		log.Errorf("failed to retrieve hash from %s: %v", txHash, err)
		return nil, err
	}

	return hash, nil
}

// verify initial request matches vspd server
func verifyResponse(serverResp, serverReq interface{}) error {
	resp, err := json.Marshal(serverResp)
	if err != nil {
		log.Warnf("failed to marshal response request: %v", err)
		return err
	}

	req, err := json.Marshal(serverReq)
	if err != nil {
		log.Warnf("failed to marshal request: %v", err)
		return err
	}

	if !bytes.Equal(resp, req) {
		log.Warnf("server response has differing request: %#v != %#v",
			resp, req)
		return fmt.Errorf("server response contains differing request")
	}

	return nil
}

func validateVSPServerSignature(resp *http.Response, pubKey, body []byte) error {
	sigStr := resp.Header.Get("VSP-Server-Signature")
	sig, err := base64.StdEncoding.DecodeString(sigStr)
	if err != nil {
		return fmt.Errorf("Error validating VSP signature: %v", err)
	}

	if !ed25519.Verify(pubKey, body, sig) {
		return errors.New("Bad signature from VSP")
	}

	return nil
}
