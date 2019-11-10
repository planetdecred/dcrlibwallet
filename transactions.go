package dcrlibwallet

import (
	"encoding/json"
	"sort"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
	"github.com/raedahgroup/dcrlibwallet/txindex"
)

const (
	// Export constants for use in mobile apps
	// since gomobile excludes fields from sub packages.
	TxFilterAll         = txindex.TxFilterAll
	TxFilterSent        = txindex.TxFilterSent
	TxFilterReceived    = txindex.TxFilterReceived
	TxFilterTransferred = txindex.TxFilterTransferred
	TxFilterStaking     = txindex.TxFilterStaking
	TxFilterCoinBase    = txindex.TxFilterCoinBase
	TxFilterRegular     = txindex.TxFilterRegular

	TxDirectionInvalid     = txhelper.TxDirectionInvalid
	TxDirectionSent        = txhelper.TxDirectionSent
	TxDirectionReceived    = txhelper.TxDirectionReceived
	TxDirectionTransferred = txhelper.TxDirectionTransferred

	TxTypeRegular        = txhelper.TxTypeRegular
	TxTypeCoinBase       = txhelper.TxTypeCoinBase
	TxTypeTicketPurchase = txhelper.TxTypeTicketPurchase
	TxTypeVote           = txhelper.TxTypeVote
	TxTypeRevocation     = txhelper.TxTypeRevocation
)

func (lw *LibWallet) GetTransaction(txHash []byte) (string, error) {
	transaction, err := lw.GetTransactionRaw(txHash)
	if err != nil {
		log.Error(err)
		return "", err
	}

	result, err := json.Marshal(transaction)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

func (lw *LibWallet) GetTransactionRaw(txHash []byte) (*Transaction, error) {
	hash, err := chainhash.NewHash(txHash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	txSummary, _, blockHash, err := lw.Wallet.TransactionSummary(lw.shutdownContext(), hash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	return lw.decodeTransactionWithTxSummary(txSummary, blockHash)
}

func (lw *LibWallet) GetTransactions(offset, limit, txFilter int32, newestFirst bool) (string, error) {
	transactions, err := lw.GetTransactionsRaw(offset, limit, txFilter, newestFirst)
	if err != nil {
		return "", err
	}

	jsonEncodedTransactions, err := json.Marshal(&transactions)
	if err != nil {
		return "", err
	}

	return string(jsonEncodedTransactions), nil
}

func (lw *LibWallet) GetTransactionsRaw(offset, limit, txFilter int32, newestFirst bool) (transactions []Transaction, err error) {
	err = lw.txDB.Read(offset, limit, txFilter, newestFirst, &transactions)
	return
}

func (mw *MultiWallet) GetTransactions(offset, limit, txFilter int32, newestFirst bool) (string, error) {
	transactions := make([]Transaction, 0)
	for _, w := range mw.wallets {
		walletTransactions, err := w.GetTransactionsRaw(offset, limit, txFilter, newestFirst)
		if err != nil {
			return "", nil
		}

		transactions = append(transactions, walletTransactions...)
	}

	// sort transaction by timestamp in descending order
	sort.Slice(transactions[:], func(i, j int) bool {
		return transactions[i].Timestamp > transactions[j].Timestamp
	})

	if len(transactions) > int(limit) && limit > 0 {
		transactions = transactions[:limit]
	}

	jsonEncodedTransactions, err := json.Marshal(&transactions)
	if err != nil {
		return "", err
	}

	return string(jsonEncodedTransactions), nil
}

func (lw *LibWallet) CountTransactions(txFilter int32) (int, error) {
	return lw.txDB.Count(txFilter, &Transaction{})
}

func CompareTxFilter(txFilter int32, txType string, txDirection int32) bool {
	return txindex.CompareTxFilter(txFilter, txType, txDirection)
}
