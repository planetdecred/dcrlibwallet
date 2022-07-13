package api

import (
	"github.com/decred/dcrdata/v7/api/types"
)

type (
	// BlockDataBasic models primary information about a block.
	BlockDataBasic struct {
		Height     uint32        `json:"height"`
		Size       uint32        `json:"size"`
		Hash       string        `json:"hash"`
		Difficulty float64       `json:"diff"`
		StakeDiff  float64       `json:"sdiff"`
		Time       types.TimeAPI `json:"time"`
		NumTx      uint32        `json:"txlength"`
		MiningFee  *int64        `json:"fees,omitempty"`
		TotalSent  *int64        `json:"total_sent,omitempty"`
		// TicketPoolInfo may be nil for side chain blocks.
		PoolInfo *types.TicketPoolInfo `json:"ticket_pool,omitempty"`
	}

	// TreasuryDetails is the current balance, spent amount, and tx count for the
	// treasury.
	TreasuryDetails struct {
		Height         int64 `json:"height"`
		MaturityHeight int64 `json:"maturity_height"`
		Balance        int64 `json:"balance"`
		TxCount        int64 `json:"output_count"`
		AddCount       int64 `json:"add_count"`
		Added          int64 `json:"added"`
		SpendCount     int64 `json:"spend_count"`
		Spent          int64 `json:"spent"`
		TBaseCount     int64 `json:"tbase_count"`
		TBase          int64 `json:"tbase"`
		ImmatureCount  int64 `json:"immature_count"`
		Immature       int64 `json:"immature"`
	}

	// BaseState are the non-iterable fields of the ExchangeState, which embeds
	// BaseState.
	BaseState struct {
		Price float64 `json:"price"`
		// BaseVolume is poorly named. This is the volume in terms of (usually) BTC,
		// not the base asset of any particular market.
		BaseVolume float64 `json:"base_volume,omitempty"`
		Volume     float64 `json:"volume,omitempty"`
		Change     float64 `json:"change,omitempty"`
		Stamp      int64   `json:"timestamp,omitempty"`
	}

	// ExchangeRates is the dcr and btc prices converted to fiat.
	ExchangeRates struct {
		BtcIndex  string               `json:"btcIndex"`
		DcrPrice  float64              `json:"dcrPrice"`
		BtcPrice  float64              `json:"btcPrice"`
		Exchanges map[string]BaseState `json:"exchanges"`
	}

	ExchangeState struct {
		BtcIndex    string                    `json:"btc_index"`
		BtcPrice    float64                   `json:"btc_fiat_price"`
		Price       float64                   `json:"price"`
		Volume      float64                   `json:"volume"`
		DcrBtc      map[string]*ExchangeState `json:"dcr_btc_exchanges"`
		FiatIndices map[string]*ExchangeState `json:"btc_indices"`
	}

	// AddressState models the adddress balances and transactions.
	AddressState struct {
		Address            string   `json:"adddress"`
		Balance            int64    `json:"balance,string"`
		TotalReceived      int64    `json:"totalReceived,string"`
		TotalSent          int64    `json:"totalSent,string"`
		UnconfirmedBalance int64    `json:"unconfirmedBalance,string"`
		UnconfirmedTxs     int64    `json:"unconfirmedTxs"`
		Txs                int32    `json:"txs"`
		TxIds              []string `json:"txids"`
	}

	XpubAddress struct {
		Address       string `json:"name"`
		Path          string `json:"path"`
		Transfers     int32  `json:"transfers"`
		Decimals      int32  `json:"decimals"`
		Balance       int64  `json:"balance,string"`
		TotalReceived int64  `json:"totalReceived,string"`
		TotalSent     int64  `json:"totalSent,string"`
	}

	// XpubBalAndTxs models xpub transactions and balance.
	XpubBalAndTxs struct {
		Xpub               string        `json:"adddress"`
		Balance            int64         `json:"balance,string"`
		TotalReceived      int64         `json:"totalReceived,string"`
		TotalSent          int64         `json:"totalSent,string"`
		UnconfirmedBalance int64         `json:"unconfirmedBalance,string"`
		UnconfirmedTxs     int64         `json:"unconfirmedTxs"`
		Txs                int32         `json:"txs"`
		TxIds              []string      `json:"txids"`
		UsedTokens         int32         `json:"usedTokens"`
		XpubAddress        []XpubAddress `json:"tokens"`
	}
)
