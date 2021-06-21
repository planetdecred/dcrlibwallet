package dcrlibwallet

import (
	"encoding/json"
	"sort"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/planetdecred/dcrlibwallet/txhelper"
	"github.com/planetdecred/dcrlibwallet/walletdata"
)

const (
	// Export constants for use in mobile apps
	// since gomobile excludes fields from sub packages.
	TxFilterAll         = walletdata.TxFilterAll
	TxFilterSent        = walletdata.TxFilterSent
	TxFilterReceived    = walletdata.TxFilterReceived
	TxFilterTransferred = walletdata.TxFilterTransferred
	TxFilterStaking     = walletdata.TxFilterStaking
	TxFilterCoinBase    = walletdata.TxFilterCoinBase
	TxFilterRegular     = walletdata.TxFilterRegular
	TxFilterMixed       = walletdata.TxFilterMixed
	TxFilterVoted       = walletdata.TxFilterVoted
	TxFilterRevoked     = walletdata.TxFilterRevoked
	TxFilterImmature    = walletdata.TxFilterImmature
	TxFilterLive        = walletdata.TxFilterLive
	TxFilterExpired     = walletdata.TxFilterExpired

	TxDirectionInvalid     = txhelper.TxDirectionInvalid
	TxDirectionSent        = txhelper.TxDirectionSent
	TxDirectionReceived    = txhelper.TxDirectionReceived
	TxDirectionTransferred = txhelper.TxDirectionTransferred

	TxTypeRegular        = txhelper.TxTypeRegular
	TxTypeCoinBase       = txhelper.TxTypeCoinBase
	TxTypeTicketPurchase = txhelper.TxTypeTicketPurchase
	TxTypeVote           = txhelper.TxTypeVote
	TxTypeRevocation     = txhelper.TxTypeRevocation
	TxTypeMixed          = txhelper.TxTypeMixed
)

func (wallet *Wallet) PublishUnminedTransactions() error {
	n, err := wallet.internal.NetworkBackend()
	if err != nil {
		log.Error(err)
		return err
	}

	return wallet.internal.PublishUnminedTransactions(wallet.shutdownContext(), n)
}

func (wallet *Wallet) GetTransaction(txHash []byte) (string, error) {
	transaction, err := wallet.GetTransactionRaw(txHash)
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

func (wallet *Wallet) GetTransactionRaw(txHash []byte) (*Transaction, error) {
	hash, err := chainhash.NewHash(txHash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	txSummary, _, blockHash, err := wallet.internal.TransactionSummary(wallet.shutdownContext(), hash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	return wallet.decodeTransactionWithTxSummary(txSummary, blockHash)
}

func (wallet *Wallet) GetTransactions(offset, limit, txFilter int32, newestFirst bool) (string, error) {
	transactions, err := wallet.GetTransactionsRaw(offset, limit, txFilter, newestFirst)
	if err != nil {
		return "", err
	}

	jsonEncodedTransactions, err := json.Marshal(&transactions)
	if err != nil {
		return "", err
	}

	return string(jsonEncodedTransactions), nil
}

func (wallet *Wallet) GetTransactionsRaw(offset, limit, txFilter int32, newestFirst bool) (transactions []Transaction, err error) {
	err = wallet.walletDataDB.Read(offset, limit, txFilter, newestFirst, wallet.GetBestBlock(), &transactions)
	return
}

func (mw *MultiWallet) GetTransactions(offset, limit, txFilter int32, newestFirst bool) (string, error) {

	transactions, err := mw.GetTransactionsRaw(offset, limit, txFilter, newestFirst)
	if err != nil {
		return "", err
	}

	jsonEncodedTransactions, err := json.Marshal(&transactions)
	if err != nil {
		return "", err
	}

	return string(jsonEncodedTransactions), nil
}

func (mw *MultiWallet) GetTransactionsRaw(offset, limit, txFilter int32, newestFirst bool) ([]Transaction, error) {
	transactions := make([]Transaction, 0)
	for _, wallet := range mw.wallets {
		walletTransactions, err := wallet.GetTransactionsRaw(offset, limit, txFilter, newestFirst)
		if err != nil {
			return nil, err
		}

		transactions = append(transactions, walletTransactions...)
	}

	// sort transaction by timestamp in descending order
	sort.Slice(transactions[:], func(i, j int) bool {
		if newestFirst {
			return transactions[i].Timestamp > transactions[j].Timestamp
		}
		return transactions[i].Timestamp < transactions[j].Timestamp
	})

	if len(transactions) > int(limit) && limit > 0 {
		transactions = transactions[:limit]
	}

	return transactions, nil
}

func (wallet *Wallet) CountTransactions(txFilter int32) (int, error) {
	return wallet.walletDataDB.Count(txFilter, wallet.GetBestBlock(), &Transaction{})
}

func (wallet *Wallet) TicketHasVotedOrRevoked(ticketHash string) (bool, error) {
	err := wallet.walletDataDB.FindOne("TicketSpentHash", ticketHash, &Transaction{})
	if err != nil {
		if err == storm.ErrNotFound {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (wallet *Wallet) TicketSpender(ticketHash string) (*Transaction, error) {
	var spender Transaction
	err := wallet.walletDataDB.FindOne("TicketSpentHash", ticketHash, &spender)
	if err != nil {
		if err == storm.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &spender, nil
}

func (wallet *Wallet) TransactionOverview() (txOverview *TransactionOverview, err error) {

	txOverview = &TransactionOverview{}

	txOverview.Sent, err = wallet.CountTransactions(TxFilterSent)
	if err != nil {
		return
	}

	txOverview.Received, err = wallet.CountTransactions(TxFilterReceived)
	if err != nil {
		return
	}

	txOverview.Transferred, err = wallet.CountTransactions(TxFilterTransferred)
	if err != nil {
		return
	}

	txOverview.Mixed, err = wallet.CountTransactions(TxFilterMixed)
	if err != nil {
		return
	}

	txOverview.Staking, err = wallet.CountTransactions(TxFilterStaking)
	if err != nil {
		return
	}

	txOverview.Coinbase, err = wallet.CountTransactions(TxFilterCoinBase)
	if err != nil {
		return
	}

	txOverview.All = txOverview.Sent + txOverview.Received + txOverview.Transferred + txOverview.Mixed +
		txOverview.Staking + txOverview.Coinbase

	return txOverview, nil
}

func (wallet *Wallet) TxMatchesFilter(tx *Transaction, txFilter int32) bool {
	switch txFilter {
	case TxFilterSent:
		return tx.Type == TxTypeRegular && tx.Direction == TxDirectionSent
	case TxFilterReceived:
		return tx.Type == TxTypeRegular && tx.Direction == TxDirectionReceived
	case TxFilterTransferred:
		return tx.Type == TxTypeRegular && tx.Direction == TxDirectionTransferred
	case TxFilterStaking:
		switch tx.Type {
		case TxTypeTicketPurchase:
			fallthrough
		case TxTypeVote:
			fallthrough
		case TxTypeRevocation:
			return true
		}

		return false
	case TxFilterCoinBase:
		return tx.Type == TxTypeCoinBase
	case TxFilterRegular:
		return tx.Type == TxTypeRegular
	case TxFilterMixed:
		return tx.Type == TxTypeMixed
	case TxFilterVoted:
		return tx.Type == TxTypeVote
	case TxFilterRevoked:
		return tx.Type == TxTypeRevocation
	case walletdata.TxFilterImmature:
		bestBlock := wallet.GetBestBlock()
		return tx.Type == TxTypeTicketPurchase &&
			(tx.BlockHeight > (bestBlock-int32(wallet.chainParams.TicketMaturity)) ||
				tx.BlockHeight == -1)
	case TxFilterLive:
		bestBlock := wallet.GetBestBlock()
		// ticket is live if we don't have the spender hash and it hasn't expired.
		// we cannot detect missed tickets over spv.
		return tx.Type == TxTypeTicketPurchase &&
			tx.TicketSpender == "" &&
			tx.BlockHeight > 0 &&
			tx.BlockHeight <= (bestBlock-int32(wallet.chainParams.TicketMaturity)) &&
			(tx.Expiry >= bestBlock || tx.Expiry == 0)
	case TxFilterExpired:
		bestBlock := wallet.GetBestBlock()
		return tx.Type == TxTypeTicketPurchase && tx.TicketSpender == "" &&
			(tx.Expiry <= bestBlock && tx.Expiry != 0)
	case TxFilterAll:
		return true
	}

	return false
}
