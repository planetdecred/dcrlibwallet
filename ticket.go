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

	"decred.org/dcrwallet/errors"
	"decred.org/dcrwallet/rpc/client/dcrd"
	w "decred.org/dcrwallet/wallet"
	"decred.org/dcrwallet/wallet/txrules"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
	"github.com/planetdecred/dcrlibwallet/utils"
)

// StakeInfo returns information about wallet stakes, tickets and their statuses.
func (wallet *Wallet) StakeInfo() (*w.StakeInfoData, error) {
	ctx := wallet.shutdownContext()
	if n, err := wallet.internal.NetworkBackend(); err == nil {
		var rpc *dcrd.RPC
		if client, ok := n.(*dcrd.RPC); ok {
			rpc = client
		}

		if rpc != nil {
			return wallet.internal.StakeInfoPrecise(ctx, rpc)
		}
	}

	return wallet.internal.StakeInfo(ctx)
}

func (wallet *Wallet) StakingOverview() (stOverview *StakingOverview, err error) {
	stOverview = &StakingOverview{}

	stOverview.Voted, err = wallet.CountTransactions(TxFilterVoted)
	if err != nil {
		return nil, err
	}

	stOverview.Revoked, err = wallet.CountTransactions(TxFilterRevoked)
	if err != nil {
		return nil, err
	}

	stOverview.Expired, err = wallet.CountTransactions(TxFilterExpired)
	if err != nil {
		return nil, err
	}

	stOverview.Live, err = wallet.CountTransactions(TxFilterLive)
	if err != nil {
		return nil, err
	}

	stOverview.Immature, err = wallet.CountTransactions(TxFilterImmature)
	if err != nil {
		return nil, err
	}

	stOverview.All = stOverview.Immature + stOverview.Live + stOverview.Voted +
		stOverview.Expired + stOverview.Revoked

	return stOverview, nil
}

func (wallet *Wallet) GetTickets(startingBlockHash, endingBlockHash []byte, targetCount int32) ([]*TicketInfo, error) {
	return wallet.getTickets(&GetTicketsRequest{
		StartingBlockHash: startingBlockHash,
		EndingBlockHash:   endingBlockHash,
		TargetTicketCount: targetCount,
	})
}

func (wallet *Wallet) GetTicketsForBlockHeightRange(startHeight, endHeight, targetCount int32) ([]*TicketInfo, error) {
	return wallet.getTickets(&GetTicketsRequest{
		StartingBlockHeight: startHeight,
		EndingBlockHeight:   endHeight,
		TargetTicketCount:   targetCount,
	})
}

func (wallet *Wallet) getTickets(req *GetTicketsRequest) (ticketInfos []*TicketInfo, err error) {
	var startBlock, endBlock *w.BlockIdentifier
	if req.StartingBlockHash != nil && req.StartingBlockHeight != 0 {
		return nil, fmt.Errorf("starting block hash and height may not be specified simultaneously")
	} else if req.StartingBlockHash != nil {
		startBlockHash, err := chainhash.NewHash(req.StartingBlockHash)
		if err != nil {
			return nil, err
		}
		startBlock = w.NewBlockIdentifierFromHash(startBlockHash)
	} else if req.StartingBlockHeight != 0 {
		startBlock = w.NewBlockIdentifierFromHeight(req.StartingBlockHeight)
	}

	if req.EndingBlockHash != nil && req.EndingBlockHeight != 0 {
		return nil, fmt.Errorf("ending block hash and height may not be specified simultaneously")
	} else if req.EndingBlockHash != nil {
		endBlockHash, err := chainhash.NewHash(req.EndingBlockHash)
		if err != nil {
			return nil, err
		}
		endBlock = w.NewBlockIdentifierFromHash(endBlockHash)
	} else if req.EndingBlockHeight != 0 {
		endBlock = w.NewBlockIdentifierFromHeight(req.EndingBlockHeight)
	}

	targetTicketCount := int(req.TargetTicketCount)
	if targetTicketCount < 0 {
		return nil, fmt.Errorf("target ticket count may not be negative")
	}

	rangeFn := func(tickets []*w.TicketSummary, block *wire.BlockHeader) (bool, error) {
		for _, t := range tickets {
			var blockHeight int32 = -1
			if block != nil {
				blockHeight = int32(block.Height)
			}

			// t.Ticket and t.Ticket.Hash are pointers, avoid using them directly
			// as they could be re-used to hold information for some other ticket.
			// See the doc on `wallet.GetTickets`.
			ticketHash, _ := chainhash.NewHash(t.Ticket.Hash[:])
			ticket := &w.TransactionSummary{
				Hash:        ticketHash,
				Transaction: t.Ticket.Transaction,
				MyInputs:    t.Ticket.MyInputs,
				MyOutputs:   t.Ticket.MyOutputs,
				Fee:         t.Ticket.Fee,
				Timestamp:   t.Ticket.Timestamp,
				Type:        t.Ticket.Type,
			}

			// t.Spender and t.Spender.Hash are pointers, avoid using them directly
			// as they could be re-used to hold information for some other ticket.
			// See the doc on `wallet.GetTickets`.
			spender := &w.TransactionSummary{}
			if t.Spender != nil {
				spenderHash, _ := chainhash.NewHash(t.Spender.Hash[:])
				spender.Hash = spenderHash
				spender.Transaction = t.Spender.Transaction
				spender.MyInputs = t.Spender.MyInputs
				spender.MyOutputs = t.Spender.MyOutputs
				spender.Fee = t.Spender.Fee
				spender.Timestamp = t.Spender.Timestamp
				spender.Type = t.Spender.Type
			}

			ticketInfos = append(ticketInfos, &TicketInfo{
				BlockHeight: blockHeight,
				Status:      ticketStatusString(t.Status),
				Ticket:      ticket,
				Spender:     spender,
			})
		}

		return (targetTicketCount > 0) && (len(ticketInfos) >= targetTicketCount), nil
	}

	var rpc *dcrd.RPC
	if n, err := wallet.internal.NetworkBackend(); err == nil {
		if client, ok := n.(*dcrd.RPC); ok {
			rpc = client
		}
	}

	ctx := wallet.shutdownContext()
	if rpc != nil {
		err = wallet.internal.GetTicketsPrecise(ctx, rpc, rangeFn, startBlock, endBlock)
	} else {
		err = wallet.internal.GetTickets(ctx, rangeFn, startBlock, endBlock)
	}

	return
}

// TicketPrice returns the price of a ticket for the next block, also known as the stake difficulty.
// May be incorrect if blockchain sync is ongoing or if blockchain is not up-to-date.
func (wallet *Wallet) TicketPrice() (*TicketPriceResponse, error) {
	ctx := wallet.shutdownContext()
	sdiff, err := wallet.internal.NextStakeDifficulty(ctx)
	if err == nil {
		_, tipHeight := wallet.internal.MainChainTip(ctx)
		resp := &TicketPriceResponse{
			TicketPrice: int64(sdiff),
			Height:      tipHeight,
		}
		return resp, nil
	}

	n, err := wallet.internal.NetworkBackend()
	if err != nil {
		return nil, err
	}

	ticketPrice, err := n.StakeDifficulty(ctx)
	if err != nil {
		return nil, translateError(err)
	}

	_, tipHeight := wallet.internal.MainChainTip(ctx)
	return &TicketPriceResponse{
		TicketPrice: int64(ticketPrice),
		Height:      tipHeight,
	}, nil
}

// PurchaseTickets purchases tickets from the wallet. Returns a slice of hashes for tickets purchased
func (wallet *Wallet) purchaseTickets(ctx context.Context, request *PurchaseTicketsRequest, vspHost string) ([]string, error) {
	var err error

	// fetch redeem script, ticket address, pool address and pool fee if vsp host isn't empty
	if vspHost != "" {
		if err = wallet.updateTicketPurchaseRequestWithVSPInfo(vspHost, request); err != nil {
			return nil, err
		}
	}

	minConf := int32(request.RequiredConfirmations)
	params := wallet.chainParams

	var ticketAddr dcrutil.Address
	if request.TicketAddress != "" {
		ticketAddr, err = dcrutil.DecodeAddress(request.TicketAddress, params)
		if err != nil {
			return nil, errors.New("Invalid ticket address")
		}
	}

	var poolAddr dcrutil.Address
	if request.PoolAddress != "" {
		poolAddr, err = dcrutil.DecodeAddress(request.PoolAddress, params)
		if err != nil {
			return nil, errors.New("Invalid pool address")
		}
	}

	if request.PoolFees > 0 && !txrules.ValidPoolFeeRate(request.PoolFees) {
		return nil, errors.New("Invalid pool fees percentage")
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

	if txFee < 0 {
		return nil, errors.New("Negative fees per KB given")
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = wallet.internal.Unlock(ctx, request.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	purchaseTicketsRequest := &w.PurchaseTicketsRequest{
		Count:                numTickets,
		SourceAccount:        request.Account,
		VotingAddress:        ticketAddr,
		MinConf:              minConf,
		Expiry:               expiry,
		VSPFeeProcess:        request.VSPFeeProcess,
		VSPFeePaymentProcess: request.VSPFeePaymentProcess,
	}

	netBackend, err := wallet.internal.NetworkBackend()
	if err != nil {
		return nil, err
	}

	purchasedTickets, err := wallet.internal.PurchaseTickets(wallet.shutdownContext(), netBackend, purchaseTicketsRequest)
	if err != nil {
		return nil, fmt.Errorf("unable to purchase tickets: %s", err.Error())
	}

	hashes := make([]string, len(purchasedTickets.TicketHashes))
	for i, hash := range purchasedTickets.TicketHashes {
		hashes[i] = hash.String()
	}

	return hashes, nil
}

func (wallet *Wallet) updateTicketPurchaseRequestWithVSPInfo(vspHost string, request *PurchaseTicketsRequest) error {
	// generate an address and get the pubkeyaddr
	address, err := wallet.CurrentAddress(0)
	if err != nil {
		return fmt.Errorf("get wallet pubkeyaddr error: %s", err.Error())
	}

	pubKeyAddr, err := wallet.AddressPubKey(address)
	if err != nil {
		return fmt.Errorf("get wallet pubkeyaddr error: %s", err.Error())
	}

	// invoke vsp api
	ticketPurchaseInfo, err := CallVSPTicketInfoAPI(vspHost, pubKeyAddr)
	if err != nil {
		return fmt.Errorf("vsp connection error: %s", err.Error())
	}
	// decode the redeem script gotten from vsp
	rs, err := hex.DecodeString(ticketPurchaseInfo.Script)
	if err != nil {
		return fmt.Errorf("invalid vsp purchase ticket response: %s", err.Error())
	}

	ctx := wallet.shutdownContext()

	// unlock wallet and import the decoded script
	lock := make(chan time.Time, 1)
	wallet.internal.Unlock(ctx, request.Passphrase, lock)
	err = wallet.internal.ImportScript(ctx, rs)
	lock <- time.Time{}
	if err != nil && !errors.Is(errors.Exist, err) {
		return fmt.Errorf("error importing vsp redeem script: %s", err.Error())
	}

	request.TicketAddress = ticketPurchaseInfo.TicketAddress
	request.PoolAddress = ticketPurchaseInfo.PoolAddress
	request.PoolFees = ticketPurchaseInfo.PoolFees

	return nil
}

func CallVSPTicketInfoAPI(vspHost, pubKeyAddr string) (ticketPurchaseInfo *VSPTicketPurchaseInfo, err error) {
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

func remaining(idx int, max, t int64) string {
	x := (max - int64(idx)) * t
	if x == 0 {
		return "imminent"
	}
	allsecs := int(time.Duration(x).Seconds())
	str := ""
	if allsecs > 604799 {
		weeks := allsecs / 604800
		allsecs %= 604800
		str += fmt.Sprintf("%dw ", weeks)
	}
	if allsecs > 86399 {
		days := allsecs / 86400
		allsecs %= 86400
		str += fmt.Sprintf("%dd ", days)
	}
	if allsecs > 3599 {
		hours := allsecs / 3600
		allsecs %= 3600
		str += fmt.Sprintf("%dh ", hours)
	}
	if allsecs > 59 {
		mins := allsecs / 60
		allsecs %= 60
		str += fmt.Sprintf("%dm ", mins)
	}
	if allsecs > 0 {
		str += fmt.Sprintf("%ds ", allsecs)
	}
	return str
}

// NextTicketPriceRemaining returns the remaning time of a ticket for the next block
func (mw *MultiWallet) NextTicketPriceRemaining(netType string) (string, error) {
	params, err := utils.ChainParams(netType)
	if err != nil {
		return "", err
	}
	bestBestBlock := mw.GetBestBlock()
	idxBlockInWindow := int(int64(bestBestBlock.Height)%params.StakeDiffWindowSize) + 1
	blockTime := params.TargetTimePerBlock.Nanoseconds()
	windowSize := params.StakeDiffWindowSize
	t := remaining(idxBlockInWindow, windowSize, blockTime)
	return t, nil
}
