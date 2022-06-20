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
)
