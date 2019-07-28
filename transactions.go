package dcrlibwallet

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"math"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrwallet/wallet"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
	"github.com/raedahgroup/dcrlibwallet/txindex"
)

const (
	TxFilterAll         int32 = 0
	TxFilterSent        int32 = 1
	TxFilterReceived    int32 = 2
	TxFilterTransferred int32 = 3
	TxFilterStaking     int32 = 4
	TxFilterCoinBase    int32 = 5

	TxDirectionInvalid     int32 = -1
	TxDirectionSent        int32 = 0
	TxDirectionReceived    int32 = 1
	TxDirectionTransferred int32 = 2

	TxTypeRegular        = "REGULAR"
	TxTypeCoinBase       = "COINBASE"
	TxTypeTicketPurchase = "TICKET_PURCHASE"
	TxTypeVote           = "VOTE"
	TxTypeRevocation     = "REVOCATION"
)

func (lw *LibWallet) TxCount(filter *txindex.ReadFilter) (int, error) {
	return lw.txIndexDB.CountTx(filter)
}

func (lw *LibWallet) IndexTransactions(startBlockHeight int32, endBlockHeight int32, afterIndexing func()) (err error) {
	defer func() {
		afterIndexing()

		// mark current end block height as last index point
		lw.txIndexDB.SaveLastIndexPoint(endBlockHeight)

		count, err := lw.TxCount(nil)
		if err != nil {
			log.Errorf("Count tx error :%v", err)
			return
		}
		log.Infof("Transaction index finished at %d, %d transaction(s) indexed in total", endBlockHeight, count)
	}()

	if startBlockHeight == -1 {
		startBlockHeight, err = lw.txIndexDB.ReadIndexingStartBlock()
		if err != nil {
			log.Errorf("Error reading block height to start tx indexing :%v", err)
			return err
		}
	}
	if startBlockHeight > endBlockHeight {
		endBlockHeight = lw.GetBestBlock()
	}

	log.Infof("Indexing transactions start height: %d, end height: %d", startBlockHeight, endBlockHeight)

	ctx, _ := contextWithShutdownCancel(context.Background())
	rangeFn := lw.parseAndIndexTransactions(ctx)

	startBlock := wallet.NewBlockIdentifierFromHeight(startBlockHeight)
	endBlock := wallet.NewBlockIdentifierFromHeight(endBlockHeight)

	return lw.wallet.GetTransactions(rangeFn, startBlock, endBlock)
}

func (lw *LibWallet) TransactionNotification(listener TransactionListener) {
	go func() {
		n := lw.wallet.NtfnServer.TransactionNotifications()
		defer n.Done()
		for {
			v := <-n.C
			for _, transaction := range v.UnminedTransactions {
				tempTransaction, err := lw.parseTxSummary(&transaction, nil)
				if err != nil {
					log.Errorf("Error ntfn parse tx: %v", err)
					return
				}

				err = lw.replaceTxIfExist(tempTransaction)
				if err != nil {
					log.Errorf("Tx ntfn replace tx err: %v", err)
				}

				log.Info("New Transaction")

				result, err := json.Marshal(tempTransaction)
				if err != nil {
					log.Error(err)
				} else {
					listener.OnTransaction(string(result))
				}
			}
			for _, block := range v.AttachedBlocks {
				listener.OnBlockAttached(int32(block.Header.Height), block.Header.Timestamp.UnixNano())
				for _, transaction := range block.Transactions {

					tempTransaction, err := lw.parseTxSummary(&transaction, nil)
					if err != nil {
						log.Errorf("Error ntfn parse tx: %v", err)
						return
					}

					err = lw.replaceTxIfExist(tempTransaction)
					if err != nil {
						log.Errorf("Incoming block replace tx error :%v", err)
						return
					}
					listener.OnTransactionConfirmed(fmt.Sprintf("%02x", reverse(transaction.Hash[:])), int32(block.Header.Height))
				}
			}
		}
	}()
}

func (lw *LibWallet) replaceTxIfExist(tx *Transaction) error {
	var oldTx Transaction
	err := lw.txIndexDB.TxDB.One("Hash", tx.Hash, &oldTx)
	if err != nil {
		if err != txindex.ErrNotFound {
			log.Errorf("Find old tx error: %v", err)
			return err
		}
	} else {
		err = lw.txIndexDB.TxDB.DeleteStruct(&oldTx)
		if err != nil {
			log.Errorf("Delete old tx error: %v", err)
			return err
		}
	}

	err = lw.txIndexDB.TxDB.Save(tx)
	if err != nil {
		log.Errorf("Save transaction error :%v", err)
		return err
	}

	return nil
}

func reverse(hash []byte) []byte {
	for i := 0; i < len(hash)/2; i++ {
		j := len(hash) - i - 1
		hash[i], hash[j] = hash[j], hash[i]
	}
	return hash
}

func (lw *LibWallet) parseTxSummary(tx *wallet.TransactionSummary, blockHash *chainhash.Hash) (*Transaction, error) {
	var inputTotal int64
	var outputTotal int64
	var amount int64

	credits := make([]TransactionCredit, len(tx.MyOutputs))
	for index, credit := range tx.MyOutputs {
		outputTotal += int64(credit.Amount)
		credits[index] = TransactionCredit{
			Index:    int32(credit.Index),
			Account:  int32(credit.Account),
			Internal: credit.Internal,
			Amount:   int64(credit.Amount),
			Address:  credit.Address.String()}
	}

	debits := make([]TransactionDebit, len(tx.MyInputs))
	for index, debit := range tx.MyInputs {
		inputTotal += int64(debit.PreviousAmount)
		debits[index] = TransactionDebit{
			Index:           int32(debit.Index),
			PreviousAccount: int32(debit.PreviousAccount),
			PreviousAmount:  int64(debit.PreviousAmount),
			AccountName:     lw.AccountName(int32(debit.PreviousAccount))}
	}

	var direction int32 = TxDirectionInvalid
	if tx.Type == wallet.TransactionTypeRegular {
		amountDifference := outputTotal - inputTotal
		if amountDifference < 0 && (float64(tx.Fee) == math.Abs(float64(amountDifference))) {
			//Transfered
			direction = TxDirectionTransferred
			amount = int64(tx.Fee)
		} else if amountDifference > 0 {
			//Received
			direction = TxDirectionReceived
			amount = outputTotal
		} else {
			//Sent
			direction = TxDirectionSent
			amount = inputTotal
			amount -= outputTotal

			amount -= int64(tx.Fee)
		}
	}

	var height int32 = -1
	if blockHash != nil {
		blockIdentifier := wallet.NewBlockIdentifierFromHash(blockHash)
		blockInfo, err := lw.wallet.BlockInfo(blockIdentifier)
		if err != nil {
			log.Error(err)
		} else {
			height = blockInfo.Height
		}
	}

	transaction := &Transaction{
		Fee:       int64(tx.Fee),
		Hash:      fmt.Sprintf("%02x", reverse(tx.Hash[:])),
		Raw:       fmt.Sprintf("%02x", tx.Transaction[:]),
		Timestamp: tx.Timestamp,
		Type:      transactionType(tx.Type),
		Credits:   &credits,
		Amount:    amount,
		Height:    height,
		Direction: direction,
		Debits:    &debits}

	return transaction, nil

}

func (lw *LibWallet) parseAndIndexTransactions(ctx context.Context) func(block *wallet.Block) (bool, error) {
	var totalIndexed int32

	return func(block *wallet.Block) (bool, error) {
		for _, txSummary := range block.Transactions {
			var blockHash *chainhash.Hash
			if block.Header != nil {
				hash := block.Header.BlockHash()
				blockHash = &hash
			} else {
				blockHash = nil
			}

			tx, err := lw.decodeTransactionWithTxSummary(&txSummary, blockHash)
			if err != nil {
				return false, err
			}

			err = lw.txIndexDB.SaveOrUpdate(tx)
			if err != nil {
				log.Errorf("Save or update tx error :%v", err)
				return false, err
			}

			totalIndexed++
			for _, syncResponse := range lw.syncProgressListeners {
				syncResponse.OnIndexTransactions(totalIndexed)
			}
		}

		if block.Header != nil {
			err := lw.txIndexDB.SaveLastIndexPoint(int32(block.Header.Height))
			if err != nil {
				log.Errorf("Error setting block height for last indexed tx: ", err)
				return false, err
			}

			log.Infof("Transaction index caught up to %d", block.Header.Height)
		}

		select {
		case <-ctx.Done():
			return true, ctx.Err()
		default:
			return false, nil
		}
	}
}

func (lw *LibWallet) GetTransaction(txHash []byte) (string, error) {
	transaction, err := lw.GetTransactionRaw(txHash)
	if err != nil {
		return "", err
	}

	result, err := json.Marshal(transaction)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

func (lw *LibWallet) GetTransactionRaw(txHash []byte) (*txhelper.Transaction, error) {
	hash, err := chainhash.NewHash(txHash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	txSummary, _, blockHash, err := lw.wallet.TransactionSummary(hash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	tx, err := lw.decodeTransactionWithTxSummary(txSummary, blockHash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	return tx, nil
}

func (lw *LibWallet) GetTransactions(offset, limit int32, filter *txindex.ReadFilter) (string, error) {
	transactions, err := lw.GetTransactionsRaw(offset, limit, filter)
	if err != nil {
		return "", nil
	}

	jsonEncodedTransactions, err := json.Marshal(&transactions)
	if err != nil {
		return "", err
	}

	return string(jsonEncodedTransactions), nil
}

func (lw *LibWallet) GetTransactionsRaw(offset, limit int32, filter *txindex.ReadFilter) (transactions []*txhelper.Transaction, err error) {
	return lw.txIndexDB.Read(offset, limit, filter)
}

func (lw *LibWallet) DecodeTransaction(txHash []byte) (string, error) {
	hash, err := chainhash.NewHash(txHash)
	if err != nil {
		log.Error(err)
		return "", err
	}

	txSummary, _, blockHash, err := lw.wallet.TransactionSummary(hash)
	if err != nil {
		log.Error(err)
		return "", err
	}

	tx, err := lw.decodeTransactionWithTxSummary(txSummary, blockHash)
	if err != nil {
		log.Error(err)
		return "", err
	}

	result, _ := json.Marshal(tx)
	return string(result), nil
}

func (lw *LibWallet) CountTransactions(txFilter int32) (int, error) {
	query := lw.prepareTxQuery(txFilter)

	count, err := query.Count(&Transaction{})
	if err != nil {
		return -1, err
	}

	return count, nil
}

func (lw *LibWallet) prepareTxQuery(txFilter int32) (query storm.Query) {
	switch txFilter {
	case TxFilterSent:
		query = lw.txIndexDB.TxDB.Select(
			q.Eq("Direction", TxDirectionSent),
		)
	case TxFilterReceived:
		query = lw.txIndexDB.TxDB.Select(
			q.Eq("Direction", TxDirectionReceived),
		)
	case TxFilterTransferred:
		query = lw.txIndexDB.TxDB.Select(
			q.Eq("Direction", TxDirectionTransferred),
		)
	case TxFilterStaking:
		query = lw.txIndexDB.TxDB.Select(
			q.Not(
				q.Eq("Type", TxTypeRegular),
				q.Eq("Type", TxTypeCoinBase),
			),
		)
	case TxFilterCoinBase:
		query = lw.txIndexDB.TxDB.Select(
			q.Eq("Type", TxTypeCoinBase),
		)
	default:
		query = lw.txIndexDB.TxDB.Select(
			q.True(),
		)
	}

	query = query.OrderBy("Timestamp").Reverse()
	return
}

func (lw *LibWallet) decodeTransactionWithTxSummary(txSummary *wallet.TransactionSummary, blockHash *chainhash.Hash) (
	*txhelper.Transaction, error) {

	var blockHeight int32 = -1
	if blockHash != nil {
		blockIdentifier := wallet.NewBlockIdentifierFromHash(blockHash)
		blockInfo, err := lw.wallet.BlockInfo(blockIdentifier)
		if err != nil {
			log.Error(err)
		} else {
			blockHeight = blockInfo.Height
		}
	}

	walletInputs := make([]*txhelper.WalletInput, len(txSummary.MyInputs))
	for i, input := range txSummary.MyInputs {
		walletInputs[i] = &txhelper.WalletInput{
			Index:    int32(input.Index),
			AmountIn: int64(input.PreviousAmount),
			WalletAccount: &txhelper.WalletAccount{
				AccountNumber: int32(input.PreviousAccount),
				AccountName:   lw.AccountName(int32(input.PreviousAccount)),
			},
		}
	}

	walletOutputs := make([]*txhelper.WalletOutput, len(txSummary.MyOutputs))
	for i, output := range txSummary.MyOutputs {
		walletOutputs[i] = &txhelper.WalletOutput{
			Index:     int32(output.Index),
			AmountOut: int64(output.Amount),
			Address:   output.Address.EncodeAddress(),
			WalletAccount: &txhelper.WalletAccount{
				AccountNumber: int32(output.Account),
				AccountName:   lw.AccountName(int32(output.Account)),
			},
		}
	}

	walletTx := &txhelper.TxInfoFromWallet{
		BlockHeight: blockHeight,
		Timestamp:   txSummary.Timestamp,
		Hex:         fmt.Sprintf("%x", txSummary.Transaction),
		Inputs:      walletInputs,
		Outputs:     walletOutputs,
	}

	return txhelper.DecodeTransaction(walletTx, lw.activeNet.Params)
}

func transactionType(txType wallet.TransactionType) string {
	switch txType {
	case wallet.TransactionTypeCoinbase:
		return TxTypeCoinBase
	case wallet.TransactionTypeTicketPurchase:
		return TxTypeTicketPurchase
	case wallet.TransactionTypeVote:
		return TxTypeVote
	case wallet.TransactionTypeRevocation:
		return TxTypeRevocation
	default:
		return TxTypeRegular
	}
}
