package dcrlibwallet

import (
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrwallet/wallet/v3"
	"github.com/raedahgroup/dcrlibwallet/txindex"
)

func (lw *LibWallet) IndexTransactions(waitGroup *sync.WaitGroup) error {
	ctx := lw.shutdownContext()

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

			_, err = lw.txDB.SaveOrUpdate(&Transaction{}, tx)
			if err != nil {
				log.Errorf("[%d] Index tx replace tx err : %v", lw.ID, err)
				return false, err
			}

			totalIndex++
		}

		if block.Header != nil {
			txEndHeight = block.Header.Height
			err := lw.txDB.SaveLastIndexPoint(int32(txEndHeight))
			if err != nil {
				log.Errorf("[%d] Set tx index end block height error: ", lw.ID, err)
				return false, err
			}

			log.Tracef("[%d] Index saved for transactions in block %d", lw.ID, txEndHeight)
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
		log.Errorf("[%d] Get tx indexing start point error: %v", lw.ID, err)
		return err
	}

	endHeight := lw.GetBestBlock()

	startBlock := wallet.NewBlockIdentifierFromHeight(beginHeight)
	endBlock := wallet.NewBlockIdentifierFromHeight(endHeight)

	defer func() {
		waitGroup.Done()
		count, err := lw.txDB.Count(txindex.TxFilterAll, &Transaction{})
		if err != nil {
			log.Errorf("[%d] Post-indexing tx count error :%v", lw.ID, err)
			return
		}
		log.Tracef("[%d] Transaction index finished at %d, %d transaction(s) indexed in total", lw.ID, txEndHeight, count)
	}()

	waitGroup.Add(1)
	log.Tracef("[%d] Indexing transactions start height: %d, end height: %d", lw.ID, beginHeight, endHeight)
	go lw.wallet.GetTransactions(ctx, rangeFn, startBlock, endBlock)
	return nil
}
