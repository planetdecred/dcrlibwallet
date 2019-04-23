package dcrlibwallet

import "encoding/json"

type TransactionListener interface {
	OnTransaction(transaction string)
	OnTransactionConfirmed(hash string, height int32)
	OnBlockAttached(height int32, timestamp int64)
}

func (lw *LibWallet) RegisterTxNotificationListener(listener TransactionListener) {
	lw.txNotificationListener = listener
}

func (lw *LibWallet) ListenForTxNotification() {
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

				err = lw.txIndexDB.SaveOrUpdate(decodedTx)
				if err != nil {
					log.Errorf("Tx ntfn replace tx err: %v", err)
				}

				log.Info("New Transaction")
				result, err := json.Marshal(decodedTx)
				if err != nil {
					log.Error(err)
				} else if lw.txNotificationListener != nil {
					lw.txNotificationListener.OnTransaction(string(result))
				}
			}

			// process mined tx gotten from notification
			for _, block := range txNotification.AttachedBlocks {
				if lw.txNotificationListener != nil {
					lw.txNotificationListener.OnBlockAttached(int32(block.Header.Height), block.Header.Timestamp.UnixNano())
				}

				blockHash := block.Header.BlockHash()
				for _, txSummary := range block.Transactions {
					decodedTx, err := lw.decodeTransactionWithTxSummary(&txSummary, &blockHash)
					if err != nil {
						log.Errorf("Incoming block decode tx err: %v", err)
						return
					}

					err = lw.txIndexDB.SaveOrUpdate(decodedTx)
					if err != nil {
						log.Errorf("Incoming block replace tx error :%v", err)
					}

					if lw.txNotificationListener != nil {
						lw.txNotificationListener.OnTransactionConfirmed(txSummary.Hash.String(), int32(block.Header.Height))
					}
				}
			}
		}
	}()
}
