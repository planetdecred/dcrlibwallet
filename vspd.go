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

// GetVspInfo returns the information of the specified VSP base URL
func (wallet *Wallet) GetVspInfo(baseURL string) (*GetVspInfoResponse, error) {
	resp, err := http.Get(baseURL + "/api/vspinfo")
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

// GetVSPFeeAddress is the first part of submiting ticket to a VSP. It returns a
// fee address and an amount that must be paid. The fee Tx details must be sent
// in the PayFee method for the submittion to be recorded
func (wallet *Wallet) GetVSPFeeAddress(baseURL, ticketHash, commitmentAddr string, passphrase,
	vspPubKey []byte) (*GetFeeAddressResponse, error) {

	req := GetFeeAddressRequest{
		TicketHash: ticketHash,
		Timestamp:  time.Now().Unix(),
	}
	url := strings.TrimSuffix(baseURL, "/") + "/api/feeaddress"
	resp, err := wallet.signedVSP_HTTP(url, http.MethodPost, commitmentAddr, passphrase, vspPubKey, req)
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
func (wallet *Wallet) PayVSPFee(baseURL, feeTx, privKeyWIF, ticketHash, commitmentAddr string,
	voteChoices map[string]string, passphrase, vspPubKey []byte) error {

	req := PayFeeRequest{
		FeeTx:       feeTx,
		VotingKey:   privKeyWIF,
		TicketHash:  ticketHash,
		Timestamp:   time.Now().Unix(),
		VoteChoices: voteChoices,
	}

	url := strings.TrimSuffix(baseURL, "/") + "/api/payfee"
	_, err := wallet.signedVSP_HTTP(url, http.MethodPost, commitmentAddr, passphrase, vspPubKey, req)
	if err != nil {
		return err
	}

	return nil
}

// GetTicketStatus returns the status of the specified ticket from the VSP
func (wallet *Wallet) GetTicketStatus(baseURL, ticketHash, commitmentAddr string, passphrase, vspPubKey []byte) error {
	req := TicketStatusRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash,
	}

	url := strings.TrimSuffix(baseURL, "/") + "/api/ticketstatus"
	_, err := wallet.signedVSP_HTTP(url, http.MethodGet, commitmentAddr, passphrase, vspPubKey, req)
	if err != nil {
		return err
	}

	return nil
}

// SetVoteChoices updates the vote choice of the specified ticket on the VSP
func (wallet *Wallet) SetVoteChoices(baseURL, ticketHash, commitmentAddr string, passphrase,
	vspPubKey []byte, choices map[string]string) error {

	req := SetVoteChoicesRequest{
		Timestamp:   time.Now().Unix(),
		TicketHash:  ticketHash,
		VoteChoices: choices,
	}

	url := strings.TrimSuffix(baseURL, "/") + "/api/setvotechoices"
	_, err := wallet.signedVSP_HTTP(url, http.MethodPost, commitmentAddr, passphrase, vspPubKey, req)
	if err != nil {
		return err
	}

	return nil
}

// signedVSP_HTTP makes a request against a VSP API. The request will be JSON
// encoded and signed using the provided commitment address. The signature of
// the response is also validated using the VSP pubkey.
func (wallet *Wallet) signedVSP_HTTP(url, method, commitmentAddr string, passphrase, vspPubKey []byte,
	request interface{}) ([]byte, error) {

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, err
	}

	signature, err := wallet.SignMessage(passphrase, commitmentAddr, string(reqBytes))
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
