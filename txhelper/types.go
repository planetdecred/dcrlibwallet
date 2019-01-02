package txhelper

import "github.com/decred/dcrd/dcrutil"

type TransactionDestination struct {
	Address string
	Amount dcrutil.Amount
}
