package txhelper

//import (
//	"github.com/decred/dcrwallet/wallet"
//	"github.com/decred/dcrd/chaincfg/chainhash"
//	"github.com/asdine/storm"
//	"context"
//)
//
//const (
//	BucketTxInfo   = "TxIndexInfo"
//	KeyEndBlock    = "EndBlock"
//	MaxReOrgBlocks = 6
//)
//
//type TxIndexRequest struct {
//	StartBlockHeight int32
//	EndBlockHeight int32
//	DbDir         string
//}
//
//func IndexTransactions(ctx context.Context, startBlockHeight int32, endBlockHeight int32, afterIndexing func()) error {
//	var totalIndex int32
//	rangeFn := func(block *wallet.Block) (bool, error) {
//		for _, transaction := range block.Transactions {
//			var blockHash *chainhash.Hash
//			if block.Header != nil {
//				hash := block.Header.BlockHash()
//				blockHash = &hash
//			} else {
//				blockHash = nil
//			}
//
//			tx, err := lw.parseTxSummary(&transaction, blockHash)
//			if err != nil {
//				return false, err
//			}
//
//			err = lw.saveOrUpdateTx(tx)
//			if err != nil {
//				log.Errorf("Save or update tx error :%v", err)
//				return false, err
//			}
//
//			totalIndex++
//			for _, syncResponse := range lw.syncProgressListeners {
//				syncResponse.OnIndexTransactions(totalIndex)
//			}
//		}
//
//		if block.Header != nil {
//			err := lw.txDB.Set(BucketTxInfo, KeyEndBlock, &endBlockHeight)
//			if err != nil {
//				log.Errorf("Error setting block height for last indexed tx: ", err)
//				return false, err
//			}
//
//			log.Infof("Transaction index caught up to %d", endBlockHeight)
//		}
//
//		select {
//		case <-ctx.Done():
//			return true, ctx.Err()
//		default:
//			return false, nil
//		}
//	}
//
//	if startBlockHeight == -1 {
//		var previousEndBlock int32
//		err := lw.txDB.Get(BucketTxInfo, KeyEndBlock, &previousEndBlock)
//		if err != nil && err != storm.ErrNotFound {
//			log.Errorf("Error reading block height for last indexed tx :%v", err)
//			return err
//		}
//
//		startBlockHeight = previousEndBlock
//		startBlockHeight -= MaxReOrgBlocks
//
//		if startBlockHeight < 0 {
//			startBlockHeight = 0
//		}
//	}
//
//	if startBlockHeight > endBlockHeight {
//		endBlockHeight = lw.GetBestBlock()
//	}
//
//	startBlock := wallet.NewBlockIdentifierFromHeight(startBlockHeight)
//	endBlock := wallet.NewBlockIdentifierFromHeight(endBlockHeight)
//
//	defer func() {
//		afterIndexing()
//		count, err := lw.txDB.Count(&Transaction{})
//		if err != nil {
//			log.Errorf("Count tx error :%v", err)
//			return
//		}
//		log.Infof("Transaction index finished at %d, %d transaction(s) indexed in total", endBlockHeight, count)
//	}()
//
//	log.Infof("Indexing transactions start height: %d, end height: %d", startBlockHeight, endBlockHeight)
//	return lw.wallet.GetTransactions(rangeFn, startBlock, endBlock)
//}
