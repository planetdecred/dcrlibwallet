package txindex

import (
	"github.com/asdine/storm/q"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
)

type ReadFilter struct {
	matcher q.Matcher
}

func Filter() *ReadFilter {
	return &ReadFilter{}
}

func (filter *ReadFilter) AndWithTxTypes(txTypes ...string) *ReadFilter {
	var filterItems []interface{}
	for _, txDirection := range txTypes {
		filterItems = append(filterItems, txDirection)
	}

	filter.matcher = filter.createMatcher("Direction", filterItems, q.And)
	return filter
}

func (filter *ReadFilter) AndForDirections(txDirections ...txhelper.TransactionDirection) *ReadFilter {
	var filterItems []interface{}
	for _, txDirection := range txDirections {
		filterItems = append(filterItems, txDirection)
	}

	filter.matcher = filter.createMatcher("Direction", filterItems, q.And)

	return filter
}

func (filter *ReadFilter) OrWithTxTypes(txTypes ...string) *ReadFilter {
	var filterItems []interface{}
	for _, txDirection := range txTypes {
		filterItems = append(filterItems, txDirection)
	}

	filter.matcher = filter.createMatcher("Direction", filterItems, q.Or)
	return filter
}

func (filter *ReadFilter) OrForDirections(txDirections ...txhelper.TransactionDirection) *ReadFilter {
	var filterItems []interface{}
	for _, txDirection := range txDirections {
		filterItems = append(filterItems, txDirection)
	}

	filter.matcher = filter.createMatcher("Direction", filterItems, q.Or)

	return filter
}

func (filter *ReadFilter) createMatcher(fieldName string, items []interface{}, joinFunc func(matchers ...q.Matcher) q.Matcher ) q.Matcher {
	if len(items) == 0 {
		return filter.matcher
	}
	var matchers []q.Matcher
	for _, item := range items {
		matchers = append(matchers, q.StrictEq(fieldName, item))
	}

	if filter.matcher != nil {
		matchers = append([]q.Matcher{filter.matcher}, matchers...)
	}

	var matcher q.Matcher
	if len(matchers) > 1 {
		matcher = joinFunc(matchers...)
	} else {
		matcher = matchers[0]
	}
	return matcher
}

func (filter *ReadFilter) AndNotForDirections(txDirections ...txhelper.TransactionDirection) *ReadFilter {
	var filterItems []interface{}
	for _, txDirection := range txDirections {
		filterItems = append(filterItems, txDirection)
	}

	filter.matcher = filter.createNotMatcher("Direction", filterItems, q.And)

	return filter
}

func (filter *ReadFilter) AndNotWithTxTypes(txTypes ...string) *ReadFilter {
	var filterItems []interface{}
	for _, txType := range txTypes {
		filterItems = append(filterItems, txType)
	}

	filter.matcher = filter.createNotMatcher("Type", filterItems, q.And)

	return filter
}

func (filter *ReadFilter) createNotMatcher(fieldName string, items []interface{}, joinFunc func(matchers ...q.Matcher) q.Matcher ) q.Matcher {
	if len(items) == 0 {
		return filter.matcher
	}
	var matchers []q.Matcher
	for _, item := range items {
		matchers = append(matchers, q.StrictEq(fieldName, item))
	}

	matcher := q.Not(matchers...)
	if filter.matcher != nil {
		matcher = q.And(filter.matcher, matcher)
	}

	return matcher
}
