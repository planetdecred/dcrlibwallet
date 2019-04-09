package dcrlibwallet

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrwallet/wallet"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
)

type TransactionListener interface {
	OnTransaction(transaction string)
	OnTransactionConfirmed(hash string, height int32)
	OnBlockAttached(height int32, timestamp int64)
}

func (lw *LibWallet) IndexTransactions(startBlockHeight int32, endBlockHeight int32, afterIndexing func()) error {
	ctx, _ := contextWithShutdownCancel(context.Background())

	var totalIndex int32
	rangeFn := func(block *wallet.Block) (bool, error) {
		for _, transaction := range block.Transactions {

			var blockHash *chainhash.Hash
			if block.Header != nil {
				hash := block.Header.BlockHash()
				blockHash = &hash
			} else {
				blockHash = nil
			}

			tx, err := lw.parseTxSummary(&transaction, blockHash)
			if err != nil {
				return false, err
			}

			err = lw.saveOrUpdateTx(tx)
			if err != nil {
				log.Errorf("Save or update tx error :%v", err)
				return false, err
			}

			totalIndex++
			for _, syncResponse := range lw.syncProgressListeners {
				syncResponse.OnIndexTransactions(totalIndex)
			}
		}

		if block.Header != nil {
			err := lw.txDB.Set(BucketTxInfo, KeyEndBlock, &endBlockHeight)
			if err != nil {
				log.Errorf("Error setting block height for last indexed tx: ", err)
				return false, err
			}

			log.Infof("Transaction index caught up to %d", endBlockHeight)
		}

		select {
		case <-ctx.Done():
			return true, ctx.Err()
		default:
			return false, nil
		}
	}

	if startBlockHeight == -1 {
		var previousEndBlock int32
		err := lw.txDB.Get(BucketTxInfo, KeyEndBlock, &previousEndBlock)
		if err != nil && err != storm.ErrNotFound {
			log.Errorf("Error reading block height for last indexed tx :%v", err)
			return err
		}

		startBlockHeight = previousEndBlock
		startBlockHeight -= MaxReOrgBlocks

		if startBlockHeight < 0 {
			startBlockHeight = 0
		}
	}

	if startBlockHeight > endBlockHeight {
		endBlockHeight = lw.GetBestBlock()
	}

	startBlock := wallet.NewBlockIdentifierFromHeight(startBlockHeight)
	endBlock := wallet.NewBlockIdentifierFromHeight(endBlockHeight)

	defer func() {
		afterIndexing()
		count, err := lw.txDB.Count(&Transaction{})
		if err != nil {
			log.Errorf("Count tx error :%v", err)
			return
		}
		log.Infof("Transaction index finished at %d, %d transaction(s) indexed in total", endBlockHeight, count)
	}()

	log.Infof("Indexing transactions start height: %d, end height: %d", startBlockHeight, endBlockHeight)
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

				err = lw.saveOrUpdateTx(tempTransaction)
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

					err = lw.saveOrUpdateTx(tempTransaction)
					if err != nil {
						log.Errorf("Incoming block replace tx error :%v", err)
						return
					}
					listener.OnTransactionConfirmed(transaction.Hash.String(), int32(block.Header.Height))
				}
			}
		}
	}()
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

func (lw *LibWallet) GetTransactionRaw(txHash []byte) (*Transaction, error) {
	hash, err := chainhash.NewHash(txHash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	txSummary, confirmations, blockHash, err := lw.wallet.TransactionSummary(hash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	tx, err := lw.parseTxSummary(txSummary, blockHash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	tx.Confirmations = confirmations
	return tx, nil
}

func (lw *LibWallet) GetTransactions(limit int32) (string, error) {
	var transactions []Transaction

	query := lw.txDB.Select().OrderBy("Timestamp").Reverse()
	if limit > 0 {
		query = query.Limit(int(limit))
	}

	err := query.Find(&transactions)
	if err != nil {
		return "", nil
	}

	jsonEncodedTransactions, err := json.Marshal(&transactions)
	if err != nil {
		return "", err
	}

	return string(jsonEncodedTransactions), nil
}

func (lw *LibWallet) DecodeTransaction(txHash []byte) (string, error) {
	hash, err := chainhash.NewHash(txHash)
	if err != nil {
		log.Error(err)
		return "", err
	}

	txSummary, _, _, err := lw.wallet.TransactionSummary(hash)
	if err != nil {
		log.Error(err)
		return "", err
	}

	tx, err := txhelper.DecodeTransaction(hash, txSummary.Transaction, lw.activeNet.Params, lw.AddressInfo)
	if err != nil {
		log.Error(err)
		return "", err
	}

	result, _ := json.Marshal(tx)
	return string(result), nil
}

func (lw *LibWallet) parseTxSummary(txSummary *wallet.TransactionSummary, blockHash *chainhash.Hash) (*Transaction, error) {
	var inputTotal int64
	var outputTotal int64

	credits := make([]*TransactionCredit, len(txSummary.MyOutputs))
	for index, credit := range txSummary.MyOutputs {
		outputTotal += int64(credit.Amount)
		credits[index] = &TransactionCredit{
			Index:    int32(credit.Index),
			Account:  int32(credit.Account),
			Internal: credit.Internal,
			Amount:   int64(credit.Amount),
			Address:  credit.Address.String()}
	}

	debits := make([]*TransactionDebit, len(txSummary.MyInputs))
	for index, debit := range txSummary.MyInputs {
		inputTotal += int64(debit.PreviousAmount)
		debits[index] = &TransactionDebit{
			Index:           int32(debit.Index),
			PreviousAccount: int32(debit.PreviousAccount),
			PreviousAmount:  int64(debit.PreviousAmount),
			AccountName:     lw.AccountName(debit.PreviousAccount)}
	}

	amount, direction := txhelper.TransactionAmountAndDirection(inputTotal, outputTotal, int64(txSummary.Fee))

	var height int32 = -1
	if txSummary != nil {
		blockIdentifier := wallet.NewBlockIdentifierFromHash(blockHash)
		blockInfo, err := lw.wallet.BlockInfo(blockIdentifier)
		if err != nil {
			log.Error(err)
		} else {
			height = blockInfo.Height
		}
	}

	return &Transaction{
		Fee:         int64(txSummary.Fee),
		Hash:        txSummary.Hash.String(),
		Raw:         fmt.Sprintf("%02x", txSummary.Transaction[:]),
		Timestamp:   txSummary.Timestamp,
		Type:        txhelper.TransactionType(txSummary.Type),
		Credits:     credits,
		Amount:      amount,
		BlockHeight: height,
		Direction:   direction,
		Debits:      debits}, nil
}

func (lw *LibWallet) saveOrUpdateTx(tx *Transaction) error {
	var oldTx Transaction
	err := lw.txDB.One("Hash", tx.Hash, &oldTx)
	if err != nil {
		if err != storm.ErrNotFound {
			log.Errorf("Find old tx error: %v", err)
			return err
		}
	} else {
		err = lw.txDB.DeleteStruct(&oldTx)
		if err != nil {
			log.Errorf("Delete old tx error: %v", err)
			return err
		}
	}

	err = lw.txDB.Save(tx)
	if err != nil {
		log.Errorf("Save transaction error :%v", err)
		return err
	}

	return nil
}
