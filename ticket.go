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

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/rpc/client/dcrd"
	wallet "github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
)

// StakeInfo returns information about wallet stakes, tickets and their statuses.
func (lw *LibWallet) StakeInfo() (*wallet.StakeInfoData, error) {
	ctx := lw.shutdownContext()
	if n, err := lw.Wallet.NetworkBackend(); err == nil {
		var rpc *dcrd.RPC
		if client, ok := n.(*dcrd.RPC); ok {
			rpc = client
		}

		if rpc != nil {
			return lw.Wallet.StakeInfoPrecise(ctx, rpc)
		}
	}

	return lw.Wallet.StakeInfo(ctx)
}

func (lw *LibWallet) GetTickets(startingBlockHash, endingBlockHash []byte, targetCount int32) ([]*TicketInfo, error) {
	return lw.getTickets(&GetTicketsRequest{
		StartingBlockHash: startingBlockHash,
		EndingBlockHash:   endingBlockHash,
		TargetTicketCount: targetCount,
	})
}

func (lw *LibWallet) GetTicketsForBlockHeightRange(startHeight, endHeight, targetCount int32) ([]*TicketInfo, error) {
	return lw.getTickets(&GetTicketsRequest{
		StartingBlockHeight: startHeight,
		EndingBlockHeight:   endHeight,
		TargetTicketCount:   targetCount,
	})
}

func (lw *LibWallet) getTickets(req *GetTicketsRequest) (ticketInfos []*TicketInfo, err error) {
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
		for _, t := range tickets {
			var blockHeight int32 = -1
			if block != nil {
				blockHeight = int32(block.Height)
			}

			// t.Ticket and t.Ticket.Hash are pointers, avoid using them directly
			// as they could be re-used to hold information for some other ticket.
			// See the doc on `wallet.GetTickets`.
			ticketHash, _ := chainhash.NewHash(t.Ticket.Hash[:])
			ticket := &wallet.TransactionSummary{
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
			spenderHash, _ := chainhash.NewHash(t.Spender.Hash[:])
			spender := &wallet.TransactionSummary{
				Hash:        spenderHash,
				Transaction: t.Spender.Transaction,
				MyInputs:    t.Spender.MyInputs,
				MyOutputs:   t.Spender.MyOutputs,
				Fee:         t.Spender.Fee,
				Timestamp:   t.Spender.Timestamp,
				Type:        t.Spender.Type,
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
	if n, err := lw.Wallet.NetworkBackend(); err == nil {
		if client, ok := n.(*dcrd.RPC); ok {
			rpc = client
		}
	}

	ctx := lw.shutdownContext()
	if rpc != nil {
		err = lw.Wallet.GetTicketsPrecise(ctx, rpc, rangeFn, startBlock, endBlock)
	} else {
		err = lw.Wallet.GetTickets(ctx, rangeFn, startBlock, endBlock)
	}

	return
}

// TicketPrice returns the price of a ticket for the next block, also known as the stake difficulty.
// May be incorrect if blockchain sync is ongoing or if blockchain is not up-to-date.
func (lw *LibWallet) TicketPrice(ctx context.Context) (*TicketPriceResponse, error) {
	sdiff, err := lw.Wallet.NextStakeDifficulty(ctx)
	if err == nil {
		_, tipHeight := lw.Wallet.MainChainTip(ctx)
		resp := &TicketPriceResponse{
			TicketPrice: int64(sdiff),
			Height:      tipHeight,
		}
		return resp, nil
	}

	n, err := lw.Wallet.NetworkBackend()
	if err != nil {
		return nil, err
	}

	ticketPrice, err := n.StakeDifficulty(ctx)
	if err != nil {
		return nil, translateError(err)
	}

	_, tipHeight := lw.Wallet.MainChainTip(ctx)
	return &TicketPriceResponse{
		TicketPrice: int64(ticketPrice),
		Height:      int32(tipHeight),
	}, nil
}

// PurchaseTickets purchases tickets from the wallet. Returns a slice of hashes for tickets purchased
func (lw *LibWallet) PurchaseTickets(ctx context.Context, request *PurchaseTicketsRequest) ([]string, error) {
	var err error

	// fetch redeem script, ticket address, pool address and pool fee if vsp host is configured
	// vspHost := lw.ReadStringConfigValueForKey(VSPHostConfigKey)
	// if vspHost != "" {
	// 	if err = lw.updateTicketPurchaseRequestWithVSPInfo(vspHost, request); err != nil {
	// 		return nil, err
	// 	}
	// }

	minConf := int32(request.RequiredConfirmations)
	params := lw.activeNet.Params

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
	ticketFee := lw.Wallet.TicketFeeIncrement()

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
	err = lw.Wallet.Unlock(ctx, request.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	purchaseTicketsRequest := &wallet.PurchaseTicketsRequest{
		Count:         numTickets,
		SourceAccount: request.Account,
		VotingAddress: ticketAddr,
		MinConf:       minConf,
		Expiry:        expiry,
	}

	netBackend, err := lw.Wallet.NetworkBackend()
	if err != nil {
		return nil, err
	}

	purchasedTickets, err := lw.Wallet.PurchaseTicketsContext(ctx, netBackend, purchaseTicketsRequest)
	if err != nil {
		return nil, fmt.Errorf("unable to purchase tickets: %s", err.Error())
	}

	hashes := make([]string, len(purchasedTickets))
	for i, hash := range purchasedTickets {
		hashes[i] = hash.String()
	}

	return hashes, nil
}

func (lw *LibWallet) UpdateTicketPurchaseRequestWithVSPInfo(vspHost string, request *PurchaseTicketsRequest) error {
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
	ticketPurchaseInfo, err := CallVSPTicketInfoAPI(vspHost, pubKeyAddr)
	if err != nil {
		return fmt.Errorf("vsp connection error: %s", err.Error())
	}

	// decode the redeem script gotten from vsp
	rs, err := hex.DecodeString(ticketPurchaseInfo.Script)
	if err != nil {
		return fmt.Errorf("invalid vsp purchase ticket response: %s", err.Error())
	}

	ctx := lw.shutdownContext()

	// unlock wallet and import the decoded script
	lock := make(chan time.Time, 1)
	lw.Wallet.Unlock(ctx, request.Passphrase, lock)
	err = lw.Wallet.ImportScript(ctx, rs)
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
