package dcrlibwallet

import (
	"context"
	"fmt"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/raedahgroup/dcrlibwallet/addresshelper"
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

func (lw *LibWallet) GetTickets(req *GetTicketsRequest) (<-chan *GetTicketsResponse, <-chan error, error) {
	var startBlock, endBlock *wallet.BlockIdentifier
	if req.StartingBlockHash != nil && req.StartingBlockHeight != 0 {
		return nil, nil, fmt.Errorf("starting block hash and height may not be specified simultaneously")
	} else if req.StartingBlockHash != nil {
		startBlockHash, err := chainhash.NewHash(req.StartingBlockHash)
		if err != nil {
			return nil, nil, err
		}
		startBlock = wallet.NewBlockIdentifierFromHash(startBlockHash)
	} else if req.StartingBlockHeight != 0 {
		startBlock = wallet.NewBlockIdentifierFromHeight(req.StartingBlockHeight)
	}

	if req.EndingBlockHash != nil && req.EndingBlockHeight != 0 {
		return nil, nil, fmt.Errorf("ending block hash and height may not be specified simultaneously")
	} else if req.EndingBlockHash != nil {
		endBlockHash, err := chainhash.NewHash(req.EndingBlockHash)
		if err != nil {
			return nil, nil, err
		}
		endBlock = wallet.NewBlockIdentifierFromHash(endBlockHash)
	} else if req.EndingBlockHeight != 0 {
		endBlock = wallet.NewBlockIdentifierFromHeight(req.EndingBlockHeight)
	}

	targetTicketCount := int(req.TargetTicketCount)
	if targetTicketCount < 0 {
		return nil, nil, fmt.Errorf("target ticket count may not be negative")
	}

	ticketCount := 0

	ch := make(chan *GetTicketsResponse)
	errCh := make(chan error)

	rangeFn := func(tickets []*wallet.TicketSummary, block *wire.BlockHeader) (bool, error) {
		resp := &GetTicketsResponse{
			BlockHeight: block.Height,
		}

		for _, t := range tickets {
			resp.TicketStatus = ticketStatus(t)
			resp.Ticket = t
			ch <- resp
		}
		ticketCount += len(tickets)

		return (targetTicketCount > 0) && (ticketCount >= targetTicketCount), nil
	}

	go func() {
		var chainClient *rpcclient.Client
		if n, err := lw.wallet.NetworkBackend(); err == nil {
			client, err := chain.RPCClientFromBackend(n)
			if err == nil {
				chainClient = client
			}
		}
		if chainClient != nil {
			errCh <- lw.wallet.GetTicketsPrecise(rangeFn, chainClient, startBlock, endBlock)
		} else {
			errCh <- lw.wallet.GetTickets(rangeFn, startBlock, endBlock)
		}
		close(errCh)
		close(ch)
	}()

	return ch, errCh, nil
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
