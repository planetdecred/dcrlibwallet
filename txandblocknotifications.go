package dcrlibwallet

import (
	"encoding/json"

	"github.com/decred/dcrwallet/errors/v2"
)

func (mw *MultiWallet) listenForTransactions(walletID int) {
	wallet := mw.wallets[walletID]
	n := wallet.internal.NtfnServer.TransactionNotifications()
	defer n.Done() // disassociate this notification client from server when this function exits.

	for {
		v := <-n.C

		for _, transaction := range v.UnminedTransactions {
			tempTransaction, err := wallet.decodeTransactionWithTxSummary(&transaction, nil)
			if err != nil {
				log.Errorf("[%d] Error ntfn parse tx: %v", wallet.ID, err)
				return
			}

			overwritten, err := wallet.txDB.SaveOrUpdate(&Transaction{}, tempTransaction)
			if err != nil {
				log.Errorf("[%d] New Tx save err: %v", wallet.ID, err)
				return
			}

			if !overwritten {
				log.Infof("[%d] New Transaction %s", wallet.ID, tempTransaction.Hash)

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
				tempTransaction, err := wallet.decodeTransactionWithTxSummary(&transaction, &blockHash)
				if err != nil {
					log.Errorf("[%d] Error ntfn parse tx: %v", wallet.ID, err)
					return
				}

				_, err = wallet.txDB.SaveOrUpdate(&Transaction{}, tempTransaction)
				if err != nil {
					log.Errorf("[%d] Incoming block replace tx error :%v", wallet.ID, err)
					return
				}
				mw.publishTransactionConfirmed(wallet.ID, transaction.Hash.String(), int32(block.Header.Height))
			}

			mw.publishBlockAttached(wallet.ID, int32(block.Header.Height))
		}
	}
}

// AddTxAndBlockNotificationListener adds a notification listener
// for new transactions and blocks received.
func (mw *MultiWallet) AddTxAndBlockNotificationListener(txAndBlockNotificationListener TxAndBlockNotificationListener, uniqueIdentifier string) error {
	mw.notificationListenersMu.Lock()
	defer mw.notificationListenersMu.Unlock()

	_, ok := mw.txAndBlockNotificationListeners[uniqueIdentifier]
	if ok {
		return errors.New(ErrListenerAlreadyExist)
	}

	mw.txAndBlockNotificationListeners[uniqueIdentifier] = txAndBlockNotificationListener

	return nil
}

// RemoveTxAndBlockNotificationListener deletes existing
// TxAndBlockNotificationListener matching uniqueIdentifier.
func (mw *MultiWallet) RemoveTxAndBlockNotificationListener(uniqueIdentifier string) {
	mw.notificationListenersMu.Lock()
	defer mw.notificationListenersMu.Unlock()

	delete(mw.txAndBlockNotificationListeners, uniqueIdentifier)
}

func (mw *MultiWallet) mempoolTransactionNotification(transaction string) {
	mw.notificationListenersMu.RLock()
	defer mw.notificationListenersMu.RUnlock()

	for _, txAndBlockNotifcationListener := range mw.txAndBlockNotificationListeners {
		txAndBlockNotifcationListener.OnTransaction(transaction)
	}
}

func (mw *MultiWallet) publishTransactionConfirmed(walletID int, transactionHash string, blockHeight int32) {
	mw.notificationListenersMu.RLock()
	defer mw.notificationListenersMu.RUnlock()

	for _, txAndBlockNotifcationListener := range mw.txAndBlockNotificationListeners {
		txAndBlockNotifcationListener.OnTransactionConfirmed(walletID, transactionHash, blockHeight)
	}
}

func (mw *MultiWallet) publishBlockAttached(walletID int, blockHeight int32) {
	mw.notificationListenersMu.RLock()
	defer mw.notificationListenersMu.RUnlock()

	for _, txAndBlockNotifcationListener := range mw.txAndBlockNotificationListeners {
		txAndBlockNotifcationListener.OnBlockAttached(walletID, blockHeight)
	}
}
