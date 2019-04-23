package dcrlibwallet

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrwallet/wallet"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
)

func (lw *LibWallet) TxCount() (int, error) {
	return lw.txIndexDB.CountTx()
}

func (lw *LibWallet) IndexTransactions(startBlockHeight int32, endBlockHeight int32, afterIndexing func()) (err error) {
	defer func() {
		afterIndexing()

		// mark current end block height as last index point
		lw.txIndexDB.SaveLastIndexPoint(endBlockHeight)

		count, err := lw.TxCount()
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
	return lw.txIndexDB.Read(offset, limit)
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
