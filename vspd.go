package dcrlibwallet

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type VSPD struct {
	baseURL             string
	sourceWallet        *Wallet
	mwRef               *MultiWallet
	sourceAccountNumber int32
	httpClient          http.Client
}

func (mw *MultiWallet) NewVSPD(baseURL string, sourceWallet *Wallet, sourceAccountNumber int32,
	httpClient http.Client) *VSPD {
	return &VSPD{
		baseURL:             baseURL,
		sourceWallet:        sourceWallet,
		mwRef:               mw,
		sourceAccountNumber: sourceAccountNumber,
		httpClient:          httpClient,
	}
}

// GetInfo returns the information of the specified VSP base URL
func (v *VSPD) GetInfo() (*GetVspInfoResponse, error) {
	resp, err := v.httpClient.Get(v.baseURL + "/api/vspinfo")
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

	var j GetVspInfoResponse
	err = json.Unmarshal(b, &j)
	if err != nil {
		return nil, err
	}

	err = validateVSPServerSignature(resp, b, j.PubKey)
	if err != nil {
		return nil, err
	}

	return &j, nil
}

func (v *VSPD) SubmitTicket(ticketHash, votingKey, commitmentAddr string, voteChoices map[string]string,
	passphrase, vspPubKey []byte) error {
	feeInfo, err := v.GetVSPFeeAddress(ticketHash, commitmentAddr, passphrase, vspPubKey)
	if err != nil {
		fmt.Errorf("cannot get fee address, %s", err.Error())
	}
	txAuthor := v.mwRef.NewUnsignedTx(v.sourceWallet, v.sourceAccountNumber)
	txAuthor.AddSendDestination(feeInfo.FeeAddress, feeInfo.FeeAmount, true)
	txHash, err := txAuthor.Broadcast(passphrase)
	if err != nil {
		return err
	}
	if err = v.PayVSPFee(string(txHash), votingKey, ticketHash, commitmentAddr, voteChoices, passphrase,
		vspPubKey); err != nil {
		return err
	}
	return nil
}

// GetVSPFeeAddress is the first part of submiting ticket to a VSP. It returns a
// fee address and an amount that must be paid. The fee Tx details must be sent
// in the PayFee method for the submittion to be recorded
func (v *VSPD) GetVSPFeeAddress(ticketHash, commitmentAddr string, passphrase,
	vspPubKey []byte) (*GetFeeAddressResponse, error) {

	req := GetFeeAddressRequest{
		TicketHash: ticketHash,
		Timestamp:  time.Now().Unix(),
	}
	resp, err := v.signedVSP_HTTP("/api/feeaddress", http.MethodPost, commitmentAddr, passphrase, vspPubKey, req)
	if err != nil {
		return nil, err
	}

	var j GetFeeAddressResponse
	err = json.Unmarshal(resp, &j)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// PayVSPFee is the second part of submitting ticket to a VSP. The fee amount is
// gotten from GetVSPFeeAddress
func (v *VSPD) PayVSPFee(feeTx, votingKey, ticketHash, commitmentAddr string,
	voteChoices map[string]string, passphrase, vspPubKey []byte) error {

	req := PayFeeRequest{
		FeeTx:       feeTx,
		VotingKey:   votingKey,
		TicketHash:  ticketHash,
		Timestamp:   time.Now().Unix(),
		VoteChoices: voteChoices,
	}

	_, err := v.signedVSP_HTTP("/api/payfee", http.MethodPost, commitmentAddr, passphrase, vspPubKey, req)
	if err != nil {
		return err
	}

	return nil
}

// GetTicketStatus returns the status of the specified ticket from the VSP
func (v *VSPD) GetTicketStatus(baseURL, ticketHash, commitmentAddr string, passphrase, vspPubKey []byte) error {
	req := TicketStatusRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash,
	}

	_, err := v.signedVSP_HTTP("/api/ticketstatus", http.MethodGet, commitmentAddr, passphrase, vspPubKey, req)
	if err != nil {
		return err
	}

	return nil
}

// SetVoteChoices updates the vote choice of the specified ticket on the VSP
func (v *VSPD) SetVoteChoices(baseURL, ticketHash, commitmentAddr string, passphrase,
	vspPubKey []byte, choices map[string]string) error {

	req := SetVoteChoicesRequest{
		Timestamp:   time.Now().Unix(),
		TicketHash:  ticketHash,
		VoteChoices: choices,
	}

	_, err := v.signedVSP_HTTP("/api/setvotechoices", http.MethodPost, commitmentAddr, passphrase, vspPubKey, req)
	if err != nil {
		return err
	}

	return nil
}

// signedVSP_HTTP makes a request against a VSP API. The request will be JSON
// encoded and signed using the provided commitment address. The signature of
// the response is also validated using the VSP pubkey.
func (v *VSPD) signedVSP_HTTP(url, method, commitmentAddr string, passphrase, vspPubKey []byte,
	request interface{}) ([]byte, error) {

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	url = strings.TrimSuffix(v.baseURL, "/") + url
	req, err := http.NewRequest(method, url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, err
	}

	signature, err := v.sourceWallet.SignMessage(passphrase, commitmentAddr, string(reqBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Add("VSP-Client-Signature", string(signature))

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

	err = validateVSPServerSignature(resp, b, vspPubKey)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func validateVSPServerSignature(resp *http.Response, body []byte, pubKey []byte) error {
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
