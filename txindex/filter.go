package txindex

import "github.com/raedahgroup/dcrlibwallet/txhelper"

type ReadFilter struct {
	typeFilter      []string
	directionFilter []txhelper.TransactionDirection
}

func Filter() *ReadFilter {
	return &ReadFilter{}
}

func (filter *ReadFilter) WithTxTypes(txTypes ...string) *ReadFilter {
	filter.typeFilter = txTypes
	return filter
}

func (filter *ReadFilter) ForDirections(txDirections ...txhelper.TransactionDirection) *ReadFilter {
	filter.directionFilter = txDirections
	return filter
}
