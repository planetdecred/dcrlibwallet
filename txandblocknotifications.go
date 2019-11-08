package dcrlibwallet

import (
	"encoding/json"
)

func (mw *MultiWallet) listenForTransactions(lw *LibWallet) {
	n := lw.wallet.NtfnServer.TransactionNotifications()
	defer n.Done() // disassociate this notification client from server when this goroutine exits.

	for {
		v := <-n.C

		for _, transaction := range v.UnminedTransactions {
			tempTransaction, err := lw.decodeTransactionWithTxSummary(&transaction, nil)
			if err != nil {
				log.Errorf("[%d] Error ntfn parse tx: %v", lw.WalletID, err)
				return
			}

			overwritten, err := lw.txDB.SaveOrUpdate(&Transaction{}, tempTransaction)
			if err != nil {
				log.Errorf("[%d] New Tx save err: %v", lw.WalletID, err)
				return
			}

			if !overwritten {
				log.Infof("[%d] New Transaction %s", lw.WalletID, tempTransaction.Hash)

				result, err := json.Marshal(tempTransaction)
				if err != nil {
					log.Error(err)
				} else {
					mw.mempoolTransactionNotification(string(result))
				}
			}
		}

		for _, block := range v.AttachedBlocks {
			blockHash := block.Header.BlockHash()
			for _, transaction := range block.Transactions {
				tempTransaction, err := lw.decodeTransactionWithTxSummary(&transaction, &blockHash)
				if err != nil {
					log.Errorf("[%d] Error ntfn parse tx: %v", lw.WalletID, err)
					return
				}

				_, err = lw.txDB.SaveOrUpdate(&Transaction{}, tempTransaction)
				if err != nil {
					log.Errorf("[%d] Incoming block replace tx error :%v", lw.WalletID, err)
					return
				}
				mw.publishTransactionConfirmed(lw.WalletID, transaction.Hash.String(), int32(block.Header.Height))
			}
		}
	}
}
