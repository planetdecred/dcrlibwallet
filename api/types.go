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
)
