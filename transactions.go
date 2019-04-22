package dcrlibwallet

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrwallet/wallet"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
)

type TransactionListener interface {
	OnTransaction(transaction string)
	OnTransactionConfirmed(hash string, height int32)
	OnBlockAttached(height int32, timestamp int64)
}

func (lw *LibWallet) IndexTransactions(startBlockHeight int32, endBlockHeight int32, afterIndexing func()) (err error) {
	defer func() {
		afterIndexing()
		count, err := lw.txDB.Count(&txhelper.Transaction{})
		if err != nil {
			log.Errorf("Count tx error :%v", err)
			return
		}
		log.Infof("Transaction index finished at %d, %d transaction(s) indexed in total", endBlockHeight, count)
	}()

	if startBlockHeight == -1 {
		startBlockHeight, err = lw.txDB.ReadIndexingStartBlock()
		if err != nil {
			log.Errorf("Error reading block height to start tx indexing :%v", err)
			return err
		}
	}
	if startBlockHeight > endBlockHeight {
		endBlockHeight = lw.GetBestBlock()
	}

	log.Infof("Indexing transactions start height: %d, end height: %d", startBlockHeight, endBlockHeight)

	startBlock := wallet.NewBlockIdentifierFromHeight(startBlockHeight)
	endBlock := wallet.NewBlockIdentifierFromHeight(endBlockHeight)

	return lw.wallet.GetTransactions(lw.parseAndIndexTransactions, startBlock, endBlock)
}

func (lw *LibWallet) parseAndIndexTransactions(block *wallet.Block) (bool, error) {
	ctx, _ := contextWithShutdownCancel(context.Background())
	var totalIndex int32

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

		err = lw.txDB.SaveOrUpdate(tx)
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
		err := lw.txDB.SaveLastIndexPoint(int32(block.Header.Height))
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

func (lw *LibWallet) TransactionNotification(listener TransactionListener) {
	go func() {
		txNotifications := lw.wallet.NtfnServer.TransactionNotifications()
		defer txNotifications.Done()

		for {
			txNotification := <-txNotifications.C

			// process unmined tx gotten from notification
			for _, txSummary := range txNotification.UnminedTransactions {
				decodedTx, err := lw.decodeTransactionWithTxSummary(&txSummary, nil)
				if err != nil {
					log.Errorf("Tx ntfn decode tx err: %v", err)
					return
				}

				err = lw.txDB.SaveOrUpdate(decodedTx)
				if err != nil {
					log.Errorf("Tx ntfn replace tx err: %v", err)
				}

				log.Info("New Transaction")
				result, err := json.Marshal(decodedTx)
				if err != nil {
					log.Error(err)
				} else {
					listener.OnTransaction(string(result))
				}
			}

			// process mined tx gotten from notification
			for _, block := range txNotification.AttachedBlocks {
				listener.OnBlockAttached(int32(block.Header.Height), block.Header.Timestamp.UnixNano())

				blockHash := block.Header.BlockHash()
				for _, txSummary := range block.Transactions {
					decodedTx, err := lw.decodeTransactionWithTxSummary(&txSummary, &blockHash)
					if err != nil {
						log.Errorf("Incoming block decode tx err: %v", err)
						return
					}

					err = lw.txDB.SaveOrUpdate(decodedTx)
					if err != nil {
						log.Errorf("Incoming block replace tx error :%v", err)
						return
					}

					listener.OnTransactionConfirmed(txSummary.Hash.String(), int32(block.Header.Height))
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

func (lw *LibWallet) GetTransactions(offset, limit int32) (string, error) {
	transactions, err := lw.GetTransactionsRaw(offset, limit)
	if err != nil {
		return "", nil
	}

	jsonEncodedTransactions, err := json.Marshal(&transactions)
	if err != nil {
		return "", err
	}

	return string(jsonEncodedTransactions), nil
}

func (lw *LibWallet) GetTransactionsRaw(offset, limit int32) (transactions []*txhelper.Transaction, err error) {
	return lw.txDB.Read(offset, limit)
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

	var totalInputAmount, totalOutputAmount int64
	walletInputs := make([]*txhelper.WalletInput, len(txSummary.MyInputs))
	for i, input := range txSummary.MyInputs {
		walletInputs[i] = &txhelper.WalletInput{
			Index:           int32(input.Index),
			PreviousAccount: int32(input.PreviousAccount),
			AccountName:     lw.AccountName(input.PreviousAccount),
		}
		totalInputAmount += int64(input.PreviousAmount)
	}
	for _, output := range txSummary.MyOutputs {
		totalOutputAmount += int64(output.Amount)
	}

	walletTx := &txhelper.TxInfoFromWallet{
		BlockHeight:       blockHeight,
		Timestamp:         txSummary.Timestamp,
		Hex:               fmt.Sprintf("%x", txSummary.Transaction),
		Confirmations:     txhelper.TxConfirmations(blockHeight, lw.GetBestBlock()),
		Inputs:            walletInputs,
		TotalOutputAmount: totalOutputAmount,
		TotalInputAmount:  totalInputAmount,
	}

	return txhelper.DecodeTransaction(walletTx, lw.activeNet.Params)
}
