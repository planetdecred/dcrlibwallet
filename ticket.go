package dcrlibwallet

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encoding/base64"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/raedahgroup/dcrlibwallet/addresshelper"
	"sync"
)

// StakeInfo returns information about wallet stakes, tickets and their statuses.
func (lw *LibWallet) StakeInfo() (*wallet.StakeInfoData, error) {
	if n, err := lw.wallet.NetworkBackend(); err == nil {
		chainClient, _ := chain.RPCClientFromBackend(n)
		if chainClient != nil {
			return lw.wallet.StakeInfoPrecise(chainClient)
		}
	}

	return lw.wallet.StakeInfo()
}

func (lw *LibWallet) GetTickets(req *GetTicketsRequest) (ticketInfos []*TicketInfo, err error) {
	var startBlock, endBlock *wallet.BlockIdentifier
	if req.StartingBlockHash != nil && req.StartingBlockHeight != 0 {
		return nil, fmt.Errorf("starting block hash and height may not be specified simultaneously")
	} else if req.StartingBlockHash != nil {
		startBlockHash, err := chainhash.NewHash(req.StartingBlockHash)
		if err != nil {
			return nil, err
		}
		startBlock = wallet.NewBlockIdentifierFromHash(startBlockHash)
	} else if req.StartingBlockHeight != 0 {
		startBlock = wallet.NewBlockIdentifierFromHeight(req.StartingBlockHeight)
	}

	if req.EndingBlockHash != nil && req.EndingBlockHeight != 0 {
		return nil, fmt.Errorf("ending block hash and height may not be specified simultaneously")
	} else if req.EndingBlockHash != nil {
		endBlockHash, err := chainhash.NewHash(req.EndingBlockHash)
		if err != nil {
			return nil, err
		}
		endBlock = wallet.NewBlockIdentifierFromHash(endBlockHash)
	} else if req.EndingBlockHeight != 0 {
		endBlock = wallet.NewBlockIdentifierFromHeight(req.EndingBlockHeight)
	}

	targetTicketCount := int(req.TargetTicketCount)
	if targetTicketCount < 0 {
		return nil, fmt.Errorf("target ticket count may not be negative")
	}

	rangeFn := func(tickets []*wallet.TicketSummary, block *wire.BlockHeader) (bool, error) {
		// todo: should not keep copies of the ticket or block pointers (or some pointer sub-component)
		// as they could be reused, see the comment to wallet.GetTickets
		for _, t := range tickets {
			ticketInfo := &TicketInfo{
				Status:  ticketStatusString(t.Status),
				Ticket:  t.Ticket,
				Spender: t.Spender,
			}
			if block != nil {
				ticketInfo.BlockHeight = block.Height
			}

			// hash loses its value after exiting this rangeFn, not sure why
			// recreating the hash instead of simply copying it fixes that.
			ticketInfo.Ticket.Hash, _ = chainhash.NewHash(t.Ticket.Hash[:])

			ticketInfos = append(ticketInfos, ticketInfo)
		}

		return (targetTicketCount > 0) && (len(ticketInfos) >= targetTicketCount), nil
	}

	var chainClient *rpcclient.Client
	if n, err := lw.wallet.NetworkBackend(); err == nil {
		chainClient, _ = chain.RPCClientFromBackend(n)
	}
	if chainClient != nil {
		err = lw.wallet.GetTicketsPrecise(rangeFn, chainClient, startBlock, endBlock)
	} else {
		err = lw.wallet.GetTickets(rangeFn, startBlock, endBlock)
	}

	return
}

// TicketPrice returns the price of a ticket for the next block, also known as the stake difficulty.
// May be incorrect if blockchain sync is ongoing or if blockchain is not up-to-date.
func (lw *LibWallet) TicketPrice(ctx context.Context) (*TicketPriceResponse, error) {
	sdiff, err := lw.wallet.NextStakeDifficulty()
	if err == nil {
		_, tipHeight := lw.wallet.MainChainTip()
		resp := &TicketPriceResponse{
			TicketPrice: int64(sdiff),
			Height:      tipHeight,
		}
		return resp, nil
	}

	n, err := lw.wallet.NetworkBackend()
	if err != nil {
		return nil, err
	}
	chainClient, err := chain.RPCClientFromBackend(n)
	if err != nil {
		return nil, translateError(err)
	}

	ticketPrice, err := n.StakeDifficulty(ctx)
	if err != nil {
		return nil, translateError(err)
	}
	_, blockHeight, err := chainClient.GetBestBlock()
	if err != nil {
		return nil, translateError(err)
	}

	return &TicketPriceResponse{
		TicketPrice: int64(ticketPrice),
		Height:      int32(blockHeight),
	}, nil
}

// PurchaseTickets purchases tickets from the wallet. Returns a slice of hashes for tickets purchased
func (lw *LibWallet) PurchaseTickets(ctx context.Context, request *PurchaseTicketsRequest) ([]string, error) {
	var err error

	// fetch redeem script+ticket address+pool address+pool fee if vsp host is provided
	if request.VSPHost != "" {
		if err = lw.updateTicketPurchaseRequestWithVSPInfo(request); err != nil {
			return nil, err
		}
	}

	// Unmarshall the received data and prepare it as input for the ticket purchase request.
	ticketPriceResponse, err := lw.TicketPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not determine ticket price, %s", err.Error())
	}

	// Use current ticket price as spend limit
	spendLimit := dcrutil.Amount(ticketPriceResponse.TicketPrice)

	minConf := int32(request.RequiredConfirmations)
	params := lw.activeNet.Params

	var ticketAddr dcrutil.Address
	if request.TicketAddress != "" {
		ticketAddr, err = addresshelper.DecodeForNetwork(request.TicketAddress, params)
		if err != nil {
			return nil, err
		}
	}

	var poolAddr dcrutil.Address
	if request.PoolAddress != "" {
		poolAddr, err = addresshelper.DecodeForNetwork(request.PoolAddress, params)
		if err != nil {
			return nil, err
		}
	}

	if request.PoolFees > 0 {
		if !txrules.ValidPoolFeeRate(request.PoolFees) {
			return nil, errors.New("Invalid pool fees percentage")
		}
	}

	if request.PoolFees > 0 && poolAddr == nil {
		return nil, errors.New("Pool fees set but no pool addresshelper given")
	}

	if request.PoolFees <= 0 && poolAddr != nil {
		return nil, errors.New("Pool fees negative or unset but pool addresshelper given")
	}

	numTickets := int(request.NumTickets)
	if numTickets < 1 {
		return nil, errors.New("Zero or negative number of tickets given")
	}

	expiry := int32(request.Expiry)
	txFee := dcrutil.Amount(request.TxFee)
	ticketFee := lw.wallet.TicketFeeIncrement()

	// Set the ticket fee if specified
	if request.TicketFee > 0 {
		ticketFee = dcrutil.Amount(request.TicketFee)
	}

	if txFee < 0 || ticketFee < 0 {
		return nil, errors.New("Negative fees per KB given")
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = lw.wallet.Unlock(request.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	purchasedTickets, err := lw.wallet.PurchaseTickets(0, spendLimit, minConf, ticketAddr, request.Account, numTickets, poolAddr,
		request.PoolFees, expiry, txFee, ticketFee)
	if err != nil {
		return nil, fmt.Errorf("unable to purchase tickets: %s", err.Error())
	}

	hashes := make([]string, len(purchasedTickets))
	for i, hash := range purchasedTickets {
		hashes[i] = hash.String()
	}

	return hashes, nil
}

func (lw *LibWallet) updateTicketPurchaseRequestWithVSPInfo(request *PurchaseTicketsRequest) error {
	// generate an address and get the pubkeyaddr
	address, err := lw.CurrentAddress(0)
	if err != nil {
		return fmt.Errorf("get wallet pubkeyaddr error: %s", err.Error())
	}
	pubKeyAddr, err := lw.AddressPubKey(address)
	if err != nil {
		return fmt.Errorf("get wallet pubkeyaddr error: %s", err.Error())
	}

	// invoke vsp api
	ticketPurchaseInfo, err := CallVSPPurchaseTicketAPI(request.VSPHost, pubKeyAddr)
	if err != nil {
		return fmt.Errorf("vsp connection error: %s", err.Error())
	}

	err = lw.decodeAndImportScript(ticketPurchaseInfo.Script, request.Passphrase)
	if err != nil {
		return fmt.Errorf("error importing vsp redeem script: %s", err.Error())
	}

	request.TicketAddress = ticketPurchaseInfo.TicketAddress
	request.PoolAddress = ticketPurchaseInfo.PoolAddress
	request.PoolFees = ticketPurchaseInfo.PoolFees

	return nil
}

func CallVSPPurchaseTicketAPI(vspHost, pubKeyAddr string) (ticketPurchaseInfo *VSPTicketPurchaseInfo, err error) {
	apiUrl := fmt.Sprintf("%s/api/v2/purchaseticket", strings.TrimSuffix(vspHost, "/"))
	data := url.Values{}
	data.Set("UserPubKeyAddr", pubKeyAddr)

	req, err := http.NewRequest("POST", apiUrl, strings.NewReader(data.Encode()))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var apiResponse map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&apiResponse)
	if err == nil {
		data := apiResponse["data"].(map[string]interface{})
		ticketPurchaseInfo = &VSPTicketPurchaseInfo{
			Script:        data["Script"].(string),
			PoolFees:      data["PoolFees"].(float64),
			PoolAddress:   data["PoolAddress"].(string),
			TicketAddress: data["TicketAddress"].(string),
		}
	}
	return
}

func (lw *LibWallet) ImportRedeemScriptsForTickets(requests []VSPTicketPurchaseInfoRequest, vspHost string,
	passphrase []byte) (errors []error) {

	var wg sync.WaitGroup

	for _, req := range requests {
		wg.Add(1)
		go func() {
			ticketPurchaseInfo, err := CallVSPTicketInfoAPI(vspHost, req, passphrase, lw.SignMessage)
			if err != nil {
				errors = append(errors, err)
				return
			}

			err = lw.decodeAndImportScript(ticketPurchaseInfo.Script, passphrase)
			if err != nil {
				errors = append(errors, err)
			}
		}()
	}

	wg.Wait()
	return
}

func CallVSPTicketInfoAPI(vspHost string, request VSPTicketPurchaseInfoRequest, passphrase []byte,
	signMessage func(passphrase []byte, address string, message string) ([]byte, error)) (*VSPTicketPurchaseInfo, error) {

	apiUrl := fmt.Sprintf("%s/api/v2/getpurchaseinfo", strings.TrimSuffix(vspHost, "/"))

	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return nil, err
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signature, err := signMessage(passphrase, request.TicketOwnerCommitmentAddr, timestamp)
	if err != nil {
		return nil, err
	}

	authData := fmt.Sprintf("TicketAuth SignedTimestamp=%s, Signature=%s, TicketHash=%s",
		timestamp,
		base64.StdEncoding.EncodeToString(signature),
		request.TicketHash)
	req.Header.Set("Authorization", authData)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResponse map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&apiResponse)
	if err != nil {
		return nil, err
	}

	data := apiResponse["data"].(map[string]interface{})
	return &VSPTicketPurchaseInfo{
		Script:        data["Script"].(string),
		PoolFees:      data["PoolFees"].(float64),
		PoolAddress:   data["PoolAddress"].(string),
		TicketAddress: data["TicketAddress"].(string),
	}, nil
}

func (lw *LibWallet) decodeAndImportScript(script string, passphrase []byte) error {
	rs, err := hex.DecodeString(script)
	if err != nil {
		return fmt.Errorf("invalid script: %s", err.Error())
	}

	// unlock wallet and import the decoded script
	lock := make(chan time.Time, 1)
	lw.wallet.Unlock(passphrase, lock)
	err = lw.wallet.ImportScript(rs)
	lock <- time.Time{}
	if err != nil && !errors.Is(errors.Exist, err) {
		return err
	}

	return nil
}
