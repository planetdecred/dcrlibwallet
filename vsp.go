package dcrlibwallet

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"decred.org/dcrwallet/v2/errors"
	"github.com/planetdecred/dcrlibwallet/internal/vsp"
)

// VSPClient loads or creates a VSP client instance for the specified host.
func (wallet *Wallet) VSPClient(host string, pubKey []byte) (*vsp.Client, error) {
	wallet.vspClientsMu.Lock()
	defer wallet.vspClientsMu.Unlock()
	client, ok := wallet.vspClients[host]
	if ok {
		return client, nil
	}

	cfg := vsp.Config{
		URL:    host,
		PubKey: base64.StdEncoding.EncodeToString(pubKey),
		Dialer: nil, // optional, but consider providing a value
		Wallet: wallet.Internal(),
	}
	client, err := vsp.New(cfg)
	if err != nil {
		return nil, err
	}
	wallet.vspClients[host] = client
	return client, nil
}

// KnownVSPs returns a list of known VSPs. This list may be updated by calling
// ReloadVSPList. This method is safe for concurrent access.
func (mw *MultiWallet) KnownVSPs() []*VSP {
	mw.vspMu.RLock()
	defer mw.vspMu.RUnlock()
	return mw.vsps // TODO: Return a copy.
}

// SaveVSP marks a VSP as known and will be susbequently included as part of
// known VSPs.
func (mw *MultiWallet) SaveVSP(host string) (err error) {
	// check if host already exists
	vspDbData := mw.getVSPDBData()
	for _, savedHost := range vspDbData.SavedHosts {
		if savedHost == host {
			return fmt.Errorf("duplicate host %s", host)
		}
	}

	// validate host network
	info, err := vspInfo(host)
	if err != nil {
		return err
	}

	// TODO: defaultVSPs() uses strings.Contains(network, vspInfo.Network).
	if info.Network != mw.NetType() {
		return fmt.Errorf("invalid net %s", info.Network)
	}

	vspDbData.SavedHosts = append(vspDbData.SavedHosts, host)
	mw.updateVSPDBData(vspDbData)

	mw.vspMu.Lock()
	mw.vsps = append(mw.vsps, &VSP{Host: host, VspInfoResponse: info})
	mw.vspMu.Unlock()

	return
}

// LastUsedVSP returns the host of the last used VSP, as saved by the
// SaveLastUsedVSP() method.
func (mw *MultiWallet) LastUsedVSP() string {
	return mw.getVSPDBData().LastUsedVSP
}

// SaveLastUsedVSP saves the host of the last used VSP.
func (mw *MultiWallet) SaveLastUsedVSP(host string) {
	vspDbData := mw.getVSPDBData()
	vspDbData.LastUsedVSP = host
	mw.updateVSPDBData(vspDbData)
}

type vspDbData struct {
	SavedHosts  []string
	LastUsedVSP string
}

func (mw *MultiWallet) getVSPDBData() *vspDbData {
	vspDbData := new(vspDbData)
	mw.ReadUserConfigValue(KnownVSPsConfigKey, vspDbData)
	return vspDbData
}

func (mw *MultiWallet) updateVSPDBData(data *vspDbData) {
	mw.SaveUserConfigValue(KnownVSPsConfigKey, data)
}

// ReloadVSPList reloads the list of known VSPs.
// This method makes multiple network calls; should be called in a goroutine
// to prevent blocking the UI thread.
func (mw *MultiWallet) ReloadVSPList(ctx context.Context) {
	log.Debugf("Reloading list of known VSPs")
	defer log.Debugf("Reloaded list of known VSPs")

	vspDbData := mw.getVSPDBData()
	vspList := make(map[string]*VspInfoResponse)
	for _, host := range vspDbData.SavedHosts {
		vspInfo, err := vspInfo(host)
		if err != nil {
			// User saved this VSP. Log an error message.
			log.Errorf("get vsp info error for %s: %v", host, err)
		} else {
			vspList[host] = vspInfo
		}
		if ctx.Err() != nil {
			return // context canceled, abort
		}
	}

	otherVSPHosts, err := defaultVSPs(mw.NetType())
	if err != nil {
		log.Debugf("get default vsp list error: %v", err)
	}
	for _, host := range otherVSPHosts {
		if _, wasAdded := vspList[host]; wasAdded {
			continue
		}
		vspInfo, err := vspInfo(host)
		if err != nil {
			log.Debugf("vsp info error for %s: %v\n", host, err) // debug only, user didn't request this VSP
		} else {
			vspList[host] = vspInfo
		}
		if ctx.Err() != nil {
			return // context canceled, abort
		}
	}

	mw.vspMu.Lock()
	mw.vsps = make([]*VSP, 0, len(vspList))
	for host, info := range vspList {
		mw.vsps = append(mw.vsps, &VSP{Host: host, VspInfoResponse: info})
	}
	mw.vspMu.Unlock()
}

func httpGet(url string, respObj interface{}) (*http.Response, []byte, error) {
	rq := new(http.Client)
	resp, err := rq.Get((url))
	if err != nil {
		return nil, nil, err
	}

	respBytes, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return resp, respBytes, fmt.Errorf("%d response from server: %v", resp.StatusCode, string(respBytes))
	}

	err = json.Unmarshal(respBytes, respObj)
	return resp, respBytes, err
}

func vspInfo(vspHost string) (*VspInfoResponse, error) {
	vspInfoResponse := new(VspInfoResponse)
	resp, respBytes, err := httpGet(vspHost+"/api/v3/vspinfo", vspInfoResponse)
	if err != nil {
		return nil, err
	}

	// Validate server response.
	sigStr := resp.Header.Get("VSP-Server-Signature")
	sig, err := base64.StdEncoding.DecodeString(sigStr)
	if err != nil {
		return nil, fmt.Errorf("error validating VSP signature: %v", err)
	}
	if !ed25519.Verify(vspInfoResponse.PubKey, respBytes, sig) {
		return nil, errors.New("bad signature from VSP")
	}

	return vspInfoResponse, nil
}

// defaultVSPs returns a list of known VSPs.
func defaultVSPs(network string) ([]string, error) {
	var vspInfoResponse map[string]*VspInfoResponse
	_, _, err := httpGet("https://api.decred.org/?c=vsp", &vspInfoResponse)
	if err != nil {
		return nil, err
	}

	// The above API does not return the pubKeys for the
	// VSPs. Only return the host since we'll still need
	// to make another API call to get the VSP pubKeys.
	vsps := make([]string, 0)
	for url, vspInfo := range vspInfoResponse {
		if strings.Contains(network, vspInfo.Network) {
			vsps = append(vsps, "https://"+url)
		}
	}
	return vsps, nil
}

// ForUnspentUnexpiredTickets performs a function on every unexpired and unspent
// ticket from the wallet.
func (v *VSP) ForUnspentUnexpiredTickets(ctx context.Context, f func(hash *chainhash.Hash) error) error {
	wal := v.w
	params := wal.Internal().ChainParams()

	iter := func(ticketSummaries []*w.TicketSummary, _ *wire.BlockHeader) (bool, error) {
		for _, ticketSummary := range ticketSummaries {
			switch ticketSummary.Status {
			case w.TicketStatusLive:
			case w.TicketStatusImmature:
			case w.TicketStatusUnspent:
			default:
				continue
			}

			ticketHash := *ticketSummary.Ticket.Hash
			err := f(&ticketHash)
			if err != nil {
				return false, err
			}
		}

		return false, nil
	}

	_, blockHeight := wal.Internal().MainChainTip(ctx)
	startBlockNum := blockHeight -
		int32(params.TicketExpiry+uint32(params.TicketMaturity)-requiredConfs)
	startBlock := w.NewBlockIdentifierFromHeight(startBlockNum)
	endBlock := w.NewBlockIdentifierFromHeight(blockHeight)
	return wal.Internal().GetTickets(ctx, iter, startBlock, endBlock)
}

// SetVoteChoice takes the provided AgendaChoices and ticket hash, checks the
// status of the ticket from the connected vsp.  The status provides the
// current vote choice so we can just update from there if need be.
func (v *VSP) SetVoteChoice(ctx context.Context, hash *chainhash.Hash, choices ...w.AgendaChoice) error {
	status, err := v.status(ctx, hash)
	if err != nil {
		if errors.Is(err, errors.Locked) {
			return err
		}
		return nil
	}
	setVoteChoices := status.VoteChoices
	update := false
	for agenda, choice := range setVoteChoices {
		for _, newChoice := range choices {
			if agenda == newChoice.AgendaID && choice != newChoice.ChoiceID {
				update = true
			}
		}
	}
	if !update {
		return nil
	}
	err = v.setVoteChoices(ctx, hash, choices)
	if err != nil {
		return err
	}
	return nil
}

func (v *VSP) setVoteChoices(ctx context.Context, ticketHash *chainhash.Hash, choices []w.AgendaChoice) error {
	wal := v.w
	params := wal.Internal().ChainParams()

	ticketTx, err := v.tx(ticketHash)
	if err != nil {
		return fmt.Errorf("failed to retrieve ticket %v: %w", ticketHash, err)
	}

	if !stake.IsSStx(ticketTx) {
		return fmt.Errorf("%v is not a ticket", ticketHash)
	}
	if len(ticketTx.TxOut) != 3 {
		return fmt.Errorf("ticket %v has multiple commitments: %s", ticketHash, "not a solo ticket")
	}

	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, params)
	if err != nil {
		return fmt.Errorf("failed to extract commitment address from %v: %w",
			ticketHash, err)
	}

	agendaChoices := make(map[string]string, len(choices))

	// Prepare agenda choice
	for _, c := range choices {
		agendaChoices[c.AgendaID] = c.ChoiceID
	}

	var resp ticketStatus
	requestBody, err := json.Marshal(&struct {
		Timestamp   int64             `json:"timestamp"`
		TicketHash  string            `json:"tickethash"`
		VoteChoices map[string]string `json:"votechoices"`
	}{
		Timestamp:   time.Now().Unix(),
		TicketHash:  ticketHash.String(),
		VoteChoices: agendaChoices,
	})
	if err != nil {
		return err
	}

	err = v.post(ctx, apiSetVoteChoices, commitmentAddr, &resp,
		json.RawMessage(requestBody))
	if err != nil {
		return err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, resp.Request) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, resp.Request)
		return fmt.Errorf("server response contains differing request")
	}

	return nil
}

func (v *VSP) status(ctx context.Context, ticketHash *chainhash.Hash) (*ticketStatus, error) {
	w := v.w
	params := w.Internal().ChainParams()

	ticketTx, err := v.tx(ticketHash)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ticket %v: %w", ticketHash, err)
	}
	if len(ticketTx.TxOut) != 3 {
		return nil, fmt.Errorf("ticket %v has multiple commitments: %s", ticketHash, "not a solo ticket")
	}

	if !stake.IsSStx(ticketTx) {
		return nil, fmt.Errorf("%v is not a ticket", ticketHash)
	}
	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, params)
	if err != nil {
		return nil, fmt.Errorf("failed to extract commitment address from %v: %w",
			ticketHash, err)
	}

	var resp ticketStatus
	requestBody, err := json.Marshal(&struct {
		TicketHash string `json:"tickethash"`
	}{
		TicketHash: ticketHash.String(),
	})
	if err != nil {
		return nil, err
	}
	err = v.post(ctx, apiTicketStatus, commitmentAddr, &resp,
		json.RawMessage(requestBody))
	if err != nil {
		return nil, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, resp.Request) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, resp.Request)
		return nil, fmt.Errorf("server response contains differing request")
	}

	return &resp, nil
}
