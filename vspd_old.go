package dcrlibwallet

// getFeeAddress is the first part of submiting ticket to a VSP. It returns a
// fee address and an amount that must be paid by calling CreateTicketFeeTx.
//func (v *VSPD) GetVSPFeeAddress(ticketHash string, passphrase []byte) (*GetFeeAddressResponse, error) {
//	if ticketHash == "" {
//		return nil, errors.New("no ticketHash provided")
//	}
//
//	txs, commitmentAddr, err := v.getTxAndAddress(ticketHash)
//	if err != nil {
//		return nil, err
//	}
//
//	txBuf, err := txs.Bytes()
//	if err != nil {
//		return nil, fmt.Errorf("failed to serialize ticket %v: %v", ticketHash, err)
//	}
//	ticketHex := hex.EncodeToString(txBuf)
//
//	parentHash := txs.TxIn[0].PreviousOutPoint.Hash
//	parentTxs, err := v.getTxFromHash(parentHash.String())
//	if err != nil {
//		return nil, fmt.Errorf("failed to retrieve parent %v of ticket %v: %v", parentHash, ticketHash, err)
//	}
//	parentTx := parentTxs[0]
//
//	// Serialize parent
//	parentTxBuf, err := parentTx.Bytes()
//	if err != nil {
//		return nil, fmt.Errorf("failed to serialize parent %v of ticket %v: %v", parentHash, ticketHash, err)
//	}
//	parentHex := hex.EncodeToString(parentTxBuf)
//
//	req, err := json.Marshal(&GetFeeAddressRequest{
//		Timestamp:  time.Now().Unix(),
//		TicketHash: ticketHash,
//		TicketHex:  ticketHex,
//		ParentHex:  parentHex,
//	})
//	if err != nil {
//		return nil, err
//	}
//
//	resp, err := v.signedVSPHTTP("/api/v3/feeaddress", commitmentAddr.String(), passphrase, json.RawMessage(req))
//	if err != nil {
//		return nil, err
//	}
//
//	var feeAddressResponse GetFeeAddressResponse
//	err = json.Unmarshal(resp, &feeAddressResponse)
//	if err != nil {
//		return nil, err
//	}
//
//	err = verifyResponse(feeAddressResponse.Request, req)
//	if err != nil {
//		return nil, err
//	}
//
//	data := &VspdTicketInfo{
//		Timestamp:  feeAddressResponse.Timestamp,
//		Hash:       ticketHash,
//		FeeAddress: feeAddressResponse.FeeAddress,
//		FeeAmount:  feeAddressResponse.FeeAmount,
//		Expiration: feeAddressResponse.Expiration,
//	}
//
//	err = v.updateVspdDBRecord(data, ticketHash)
//	if err != nil {
//		return nil, err
//	}
//
//	return &feeAddressResponse, nil
//}

// CreateTicketFeeTx gets fee info from GetVSPFeeAddress makes payment and returns tx hash for PayVSPFee
// ticket verification
//func (v *VSPD) createTicketFeeTx(feeAmount int64, ticketHash, feeAddress string) (string, error) {
//	if ticketHash == "" || feeAmount == 0 || feeAddress == "" {
//		return "", errors.New("missing required parameters")
//	}
//
//	record, err := v.getVspdDBRecord(ticketHash, &VspdTicketInfo{})
//	if err != nil {
//		return "", err
//	}
//
//	feeTx := reflect.Indirect(*record).FieldByName("FeeTx").String()
//	if feeTx != "" {
//		return "", errors.New("vspd ticket fee has been created. Use PayVSPFee() to register ticket")
//	}
//
//	txAuthor := v.mwRef.NewUnsignedTx(v.w, int32(v.purchaseAccount))
//	txAuthor.AddSendDestination(feeAddress, feeAmount, false)
//
//	unsignedTx, err := txAuthor.constructTransaction()
//	if err != nil {
//		return "", translateError(err)
//	}
//
//	if unsignedTx.ChangeIndex >= 0 {
//		unsignedTx.RandomizeChangePosition()
//	}
//
//	txBuf := new(bytes.Buffer)
//	txBuf.Grow(unsignedTx.Tx.SerializeSize())
//	err = unsignedTx.Tx.Serialize(txBuf)
//	if err != nil {
//		return "", fmt.Errorf("failed to serialize fee transaction: %v", err)
//	}
//
//	//update db with feetx data
//	data := &VspdTicketInfo{
//		Hash:  ticketHash,
//		FeeTx: hex.EncodeToString(txBuf.Bytes()),
//	}
//
//	err = v.updateVspdDBRecord(data, ticketHash)
//	if err != nil {
//		return "", err
//	}
//
//	return hex.EncodeToString(txBuf.Bytes()), nil
//}


//// PayVSPFee is the second part of submitting ticket to a VSP. The feeTx is gotten from CreateTicketFeeTx
//// and feeAddress from GetVSPFeeAddress
//func (v *VSPD) payVSPFee(feeTx, ticketHash, feeAddress string, passphrase []byte) (*PayFeeResponse, error) {
//	if ticketHash == "" {
//		return nil, errors.New("no ticketHash provided")
//	}
//
//	if feeTx == "" {
//		return nil, errors.New("no feeTx provided")
//	}
//
//	txs, commitmentAddr, err := v.getTxAndAddress(ticketHash)
//	if err != nil {
//		return nil, err
//	}
//
//	_, votingAddress, _, err := txscript.ExtractPkScriptAddrs(0, txs.TxOut[0].PkScript, v.params, true)
//	if err != nil {
//		return nil, fmt.Errorf("failed to get voting Address: %v", err)
//	}
//
//	if len(votingAddress) == 0 {
//		return nil, errors.New("voting address not found")
//	}
//
//	votingKey, err := v.w.internal.DumpWIFPrivateKey(v.w.shutdownContext(), votingAddress[0])
//	if err != nil {
//		return nil, fmt.Errorf("failed to get votingKeyWIF for %v: %v", votingAddress[0], err)
//	}
//
//	agendaChoices, err := v.getAgenda(ticketHash)
//	if err != nil {
//		return nil, err
//	}
//
//	voteChoices := make(map[string]string)
//	for _, agendaChoice := range agendaChoices {
//		voteChoices[agendaChoice.AgendaID] = agendaChoice.ChoiceID
//	}
//
//	req, err := json.Marshal(&PayFeeRequest{
//		FeeTx:       feeTx,
//		VotingKey:   votingKey,
//		TicketHash:  ticketHash,
//		Timestamp:   time.Now().Unix(),
//		VoteChoices: voteChoices,
//	})
//	if err != nil {
//		return nil, err
//	}
//
//	resp, err := v.signedVSPHTTP("/api/v3/payfee", commitmentAddr.String(), passphrase, json.RawMessage(req))
//	if err != nil {
//		return nil, err
//	}
//
//	var payFeeResponse PayFeeResponse
//	err = json.Unmarshal(resp, &payFeeResponse)
//	if err != nil {
//		return nil, err
//	}
//
//	err = verifyResponse(payFeeResponse.Request, req)
//	if err != nil {
//		return nil, err
//	}
//
//	data := &VspdTicketInfo{
//		Timestamp:   payFeeResponse.Timestamp,
//		Hash:        ticketHash,
//		FeeTx:       feeTx,
//		VoteChoices: voteChoices,
//	}
//
//	err = v.updateVspdDBRecord(data, ticketHash)
//	if err != nil {
//		return nil, err
//	}
//
//	return &payFeeResponse, nil
//}
//
//// GetTicketStatus returns the status of the specified ticket from the VSP, after calling PayVSPFee
//func (v *VSPD) GetTicketStatus(ticketHash string, passphrase []byte) (*TicketStatusResponse, error) {
//	if ticketHash == "" {
//		return nil, errors.New("no ticketHash provided")
//	}
//
//	_, commitmentAddr, err := v.getTxAndAddress(ticketHash)
//	if err != nil {
//		return nil, err
//	}
//
//	req, err := json.Marshal(&TicketStatusRequest{
//		TicketHash: ticketHash,
//	})
//	if err != nil {
//		return nil, err
//	}
//
//	resp, err := v.signedVSPHTTP("/api/v3/ticketstatus", commitmentAddr.String(), passphrase, json.RawMessage(req))
//	if err != nil {
//		return nil, err
//	}
//
//	var ticketStatusResponse TicketStatusResponse
//	err = json.Unmarshal(resp, &ticketStatusResponse)
//	if err != nil {
//		return nil, err
//	}
//
//	err = verifyResponse(ticketStatusResponse.Request, req)
//	if err != nil {
//		return nil, err
//	}
//
//	data := &VspdTicketInfo{
//		Timestamp:       ticketStatusResponse.Timestamp,
//		Hash:            ticketHash,
//		TicketConfirmed: ticketStatusResponse.TicketConfirmed,
//		VoteChoices:     ticketStatusResponse.VoteChoices,
//		FeeTxStatus:     ticketStatusResponse.FeeTxStatus,
//		FeeTxHash:       ticketStatusResponse.FeeTxHash,
//	}
//
//	err = v.updateVspdDBRecord(data, ticketHash)
//	if err != nil {
//		return nil, err
//	}
//
//	return &ticketStatusResponse, nil
//}
//
//// SetVoteChoices updates the vote choice of the specified ticket on the VSP, after calling PayVSPFee
//func (v *VSPD) SetVoteChoices(ticketHash string, passphrase []byte, choices map[string]string) (*SetVoteChoicesResponse, error) {
//	if ticketHash == "" {
//		return nil, fmt.Errorf("no ticketHash provided")
//	}
//
//	_, commitmentAddr, err := v.getTxAndAddress(ticketHash)
//	if err != nil {
//		return nil, err
//	}
//
//	req, err := json.Marshal(&SetVoteChoicesRequest{
//		Timestamp:   time.Now().Unix(),
//		TicketHash:  ticketHash,
//		VoteChoices: choices,
//	})
//	if err != nil {
//		return nil, err
//	}
//
//	resp, err := v.signedVSPHTTP("/api/v3/setvotechoices", commitmentAddr.String(), passphrase, json.RawMessage(req))
//	if err != nil {
//		return nil, err
//	}
//
//	var setVoteChoicesResponse SetVoteChoicesResponse
//	err = json.Unmarshal(resp, &setVoteChoicesResponse)
//	if err != nil {
//		return nil, err
//	}
//
//	err = verifyResponse(setVoteChoicesResponse.Request, req)
//	if err != nil {
//		return nil, err
//	}
//
//	data := &VspdTicketInfo{
//		Timestamp:   setVoteChoicesResponse.Timestamp,
//		Hash:        ticketHash,
//		VoteChoices: choices,
//	}
//
//	err = v.updateVspdDBRecord(data, ticketHash)
//	if err != nil {
//		return nil, err
//	}
//
//	return &setVoteChoicesResponse, nil
//}
//
//// signedVSPHTTP makes a request against a VSP API. The request will be JSON
//// encoded and signed using the provided commitment address. The signature of
//// the response is also validated using the VSP pubkey.
//func (v *VSPD) signedVSPHTTP(path, commitmentAddr string, passphrase []byte, request interface{}) ([]byte, error) {
//	var reqBody io.Reader
//	reqBytes, err := json.Marshal(request)
//	if err != nil {
//		return nil, err
//	}
//	reqBody = bytes.NewReader(reqBytes)
//
//	path = strings.TrimSuffix(v.baseURL, "/") + path
//	ctx := v.w.shutdownContext()
//	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, reqBody)
//	if err != nil {
//		return nil, err
//	}
//
//	signature, err := v.w.SignMessage(passphrase, commitmentAddr, string(reqBytes))
//	if err != nil {
//		return nil, err
//	}
//
//	req.Header.Add("VSP-Client-Signature", base64.StdEncoding.EncodeToString(signature))
//
//	resp, err := v.httpClient.Do(req)
//	if err != nil {
//		return nil, err
//	}
//
//	b, err := ioutil.ReadAll(resp.Body)
//	resp.Body.Close()
//	if err != nil {
//		return nil, err
//	}
//
//	if resp.StatusCode != http.StatusOK {
//		return nil, fmt.Errorf("vsp responded with an error: %v", string(b))
//	}
//
//	err = validateVSPServerSignature(resp, v.pubKey, b)
//	if err != nil {
//		return nil, err
//	}
//
//	return b, nil
//}
//
//func (v *VSPD) getTxAndAddress(ticketHash string) (*wire.MsgTx, dcrutil.Address, error) {
//	txs, err := v.getTxFromHash(ticketHash)
//	if err != nil {
//		return nil, nil, err
//	}
//
//	if len(txs) == 0 {
//		return nil, nil, fmt.Errorf("ticket has not found: %s", ticketHash)
//	}
//
//	tx := txs[0]
//	ticketCommitmentOutput := tx.TxOut[1]
//	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketCommitmentOutput.PkScript, v.params)
//	if err != nil {
//		return nil, nil, fmt.Errorf("failed to extract script addr from %v: %v", ticketHash, err)
//	}
//
//	return tx, commitmentAddr, nil
//}
//
//// insert or update VSPD ticket info
//func (v *VSPD) updateVspdDBRecord(data interface{}, ticketHash string) error {
//	updated, err := v.w.walletDataDB.SaveOrUpdateVspdRecord(&VspdTicketInfo{}, data)
//	if err != nil {
//		return fmt.Errorf("[%d] new vspd save err : %v", v.w.ID, err)
//	}
//
//	if !updated {
//		log.Infof("[%d] New vspd ticket %s added", v.w.ID, ticketHash)
//	} else {
//		log.Infof("[%d] vspd ticket %s updated", v.w.ID, ticketHash)
//	}
//
//	return nil
//}
//
//// get VSPD ticket info
//func (v *VSPD) getVspdDBRecord(txHash string, dataPointer interface{}) (*reflect.Value, error) {
//	err := v.w.walletDataDB.FindOne("Hash", txHash, dataPointer)
//	if err != nil {
//		if err == storm.ErrNotFound {
//			return nil, fmt.Errorf("vspd ticket: " + txHash + " " + err.Error() + ". ensure you first call GetVSPFeeAddress()")
//		}
//
//		return nil, fmt.Errorf("failed to find vspd ticket for %v: %v", txHash, err)
//	}
//
//	val := reflect.ValueOf(dataPointer)
//	return &val, nil
//}
//
//// get VSPD ticket info
//func (v *VSPD) getAgenda(txHash string) ([]w.AgendaChoice, error) {
//	hash, err := getHashFromStr(txHash)
//	if err != nil {
//		return nil, err
//	}
//
//	agendaChoices, _, err := v.w.internal.AgendaChoices(v.w.shutdownContext(), hash)
//	if err != nil {
//		return nil, fmt.Errorf("failed to retrieve agenda choices for %v: %v", txHash, err)
//	}
//
//	return agendaChoices, nil
//}
//
//func (v *VSPD) getTxFromHash(txHash string) ([]*wire.MsgTx, error) {
//	hash, err := getHashFromStr(txHash)
//	if err != nil {
//		return nil, err
//	}
//
//	txs, _, err := v.w.internal.GetTransactionsByHashes(v.w.shutdownContext(), []*chainhash.Hash{hash})
//	if err != nil {
//		return nil, fmt.Errorf("failed to retrieve transaction for %v: %v", hash, err)
//	}
//
//	return txs, nil
//}
//
//func getHashFromStr(txHash string) (*chainhash.Hash, error) {
//	hash, err := chainhash.NewHashFromStr(txHash)
//	if err != nil {
//		return nil, fmt.Errorf("failed to retrieve hash from %s: %v", txHash, err)
//	}
//
//	return hash, nil
//}
//
//// verify initial request matches vspd server
//func verifyResponse(serverResp, serverReq interface{}) error {
//	resp, err := json.Marshal(serverResp)
//	if err != nil {
//		return fmt.Errorf("failed to marshal response request: %v", err)
//	}
//
//	req, err := json.Marshal(serverReq)
//	if err != nil {
//		return fmt.Errorf("failed to marshal request: %v", err)
//	}
//
//	if !bytes.Equal(req, resp) {
//		log.Warnf("server response has differing request: %#v != %#v",
//			serverResp, serverReq)
//		return fmt.Errorf("server response contains differing request")
//	}
//
//	return nil
//}
//
