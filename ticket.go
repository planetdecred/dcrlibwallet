package dcrlibwallet

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"decred.org/dcrwallet/errors"
	"github.com/planetdecred/dcrlibwallet/utils"
)

func (wallet *Wallet) TotalStakingRewards() (int64, error) {
	voteTransactions, err := wallet.GetTransactionsRaw(0, 0, TxFilterVoted, true)
	if err != nil {
		return 0, err
	}

	var totalRewards int64
	for _, tx := range voteTransactions {
		totalRewards += tx.VoteReward
	}

	return totalRewards, nil
}

func (mw *MultiWallet) TotalStakingRewards() (int64, error) {
	var totalRewards int64
	for _, wal := range mw.wallets {
		walletTotalRewards, err := wal.TotalStakingRewards()
		if err != nil {
			return 0, err
		}

		totalRewards += walletTotalRewards
	}

	return totalRewards, nil
}

func (mw *MultiWallet) TicketMaturity() int32 {
	return int32(mw.chainParams.TicketMaturity)
}

func (mw *MultiWallet) TicketExpiry() int32 {
	return int32(mw.chainParams.TicketExpiry)
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

	stOverview.Live, err = wallet.CountTransactions(TxFilterLive)
	if err != nil {
		return nil, err
	}

	stOverview.Immature, err = wallet.CountTransactions(TxFilterImmature)
	if err != nil {
		return nil, err
	}

	stOverview.Expired, err = wallet.CountTransactions(TxFilterExpired)
	if err != nil {
		return nil, err
	}

	stOverview.Unmined, err = wallet.CountTransactions(TxFilterUnmined)
	if err != nil {
		return nil, err
	}

	stOverview.All = stOverview.Unmined + stOverview.Immature + stOverview.Live + stOverview.Voted +
		stOverview.Revoked + stOverview.Expired

	return stOverview, nil
}

func (mw *MultiWallet) StakingOverview() (stOverview *StakingOverview, err error) {
	stOverview = &StakingOverview{}

	for _, wallet := range mw.wallets {
		st, err := wallet.StakingOverview()
		if err != nil {
			return nil, err
		}

		stOverview.Unmined += st.Unmined
		stOverview.Immature += st.Immature
		stOverview.Live += st.Live
		stOverview.Voted += st.Voted
		stOverview.Revoked += st.Revoked
		stOverview.Expired += st.Expired
	}

	stOverview.All = stOverview.Unmined + stOverview.Immature + stOverview.Live + stOverview.Voted +
		stOverview.Revoked + stOverview.Expired

	return stOverview, nil
}

// TicketPrice returns the price of a ticket for the next block, also known as the stake difficulty.
// May be incorrect if blockchain sync is ongoing or if blockchain is not up-to-date.
func (wallet *Wallet) TicketPrice() (*TicketPriceResponse, error) {
	ctx := wallet.shutdownContext()
	sdiff, err := wallet.internal.NextStakeDifficulty(ctx)
	if err != nil {
		return nil, err
	}

	_, tipHeight := wallet.internal.MainChainTip(ctx)
	resp := &TicketPriceResponse{
		TicketPrice: int64(sdiff),
		Height:      tipHeight,
	}
	return resp, nil
}

func (mw *MultiWallet) TicketPrice() (*TicketPriceResponse, error) {
	bestBlock := mw.GetBestBlock()
	for _, wal := range mw.wallets {
		resp, err := wal.TicketPrice()
		if err != nil {
			return nil, err
		}

		if resp.Height == bestBlock.Height {
			return resp, nil
		}
	}

	return nil, errors.New(ErrWalletNotFound)
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

// NextTicketPriceRemaining returns the remaning time in seconds of a ticket for the next block,
// if secs equal 0 is imminent
func (mw *MultiWallet) NextTicketPriceRemaining() (secs int64, err error) {
	params, er := utils.ChainParams(mw.chainParams.Name)
	if er != nil {
		secs, err = -1, er
		return
	}
	bestBestBlock := mw.GetBestBlock()
	idxBlockInWindow := int(int64(bestBestBlock.Height)%params.StakeDiffWindowSize) + 1
	blockTime := params.TargetTimePerBlock.Nanoseconds()
	windowSize := params.StakeDiffWindowSize
	x := (windowSize - int64(idxBlockInWindow)) * blockTime
	if x == 0 {
		secs, err = 0, nil
		return
	}
	secs, err = int64(time.Duration(x).Seconds()), nil
	return
}
