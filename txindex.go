package dcrlibwallet

import (
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	wallet "github.com/decred/dcrwallet/wallet/v3"
	"github.com/raedahgroup/dcrlibwallet/txindex"
)

func (lw *LibWallet) IndexTransactions(waitGroup *sync.WaitGroup) error {
	ctx, _ := lw.contextWithShutdownCancel()

	var totalIndex int32
	var txEndHeight uint32
	rangeFn := func(block *wallet.Block) (bool, error) {
		for _, transaction := range block.Transactions {

			var blockHash *chainhash.Hash
			if block.Header != nil {
				hash := block.Header.BlockHash()
				blockHash = &hash
			} else {
				blockHash = nil
			}

			tx, err := lw.decodeTransactionWithTxSummary(&transaction, blockHash)
			if err != nil {
				return false, err
			}

			err = lw.txDB.SaveOrUpdate(&Transaction{}, tx)
			if err != nil {
				log.Errorf("[%d] Index tx replace tx err :%v", lw.WalletID, err)
				return false, err
			}

			totalIndex++
		}

		if block.Header != nil {
			txEndHeight = block.Header.Height
			err := lw.txDB.SaveLastIndexPoint(int32(txEndHeight))
			if err != nil {
				log.Errorf("[%d] Set tx index end block height error: ", lw.WalletID, err)
				return false, err
			}

			log.Infof("[%d] Index saved for transactions in block %d", lw.WalletID, txEndHeight)
		}

		select {
		case <-ctx.Done():
			return true, ctx.Err()
		default:
			return false, nil
		}
	}

	beginHeight, err := lw.txDB.ReadIndexingStartBlock()
	if err != nil {
		log.Errorf("[%d] Get tx indexing start point error: %v", lw.WalletID, err)
		return err
	}

	endHeight := lw.GetBestBlock()

	startBlock := wallet.NewBlockIdentifierFromHeight(beginHeight)
	endBlock := wallet.NewBlockIdentifierFromHeight(endHeight)

	defer func() {
		waitGroup.Done()
		count, err := lw.txDB.Count(txindex.TxFilterAll, &Transaction{})
		if err != nil {
			log.Errorf("[%d] Post-indexing tx count error :%v", lw.WalletID, err)
			return
		}
		log.Infof("[%d] Transaction index finished at %d, %d transaction(s) indexed in total", lw.WalletID, txEndHeight, count)
	}()

	waitGroup.Add(1)
	log.Infof("[%d] Indexing transactions start height: %d, end height: %d", lw.WalletID, beginHeight, endHeight)
	go lw.wallet.GetTransactions(rangeFn, startBlock, endBlock)
	return nil
}
