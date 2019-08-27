package dcrlibwallet

import (
	"encoding/json"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
	"github.com/raedahgroup/dcrlibwallet/txindex"
)

type TransactionListener interface {
	OnTransaction(transaction string)
	OnTransactionConfirmed(hash string, height int32)
	OnBlockAttached(height int32, timestamp int64)
}

const (
	// Export constants for use in mobile apps
	// since gomobile excludes fields from sub packages.
	TxFilterAll         = txindex.TxFilterAll
	TxFilterSent        = txindex.TxFilterSent
	TxFilterReceived    = txindex.TxFilterReceived
	TxFilterTransferred = txindex.TxFilterTransferred
	TxFilterStaking     = txindex.TxFilterStaking
	TxFilterCoinBase    = txindex.TxFilterCoinBase

	TxDirectionInvalid     = txhelper.TxDirectionInvalid
	TxDirectionSent        = txhelper.TxDirectionSent
	TxDirectionReceived    = txhelper.TxDirectionReceived
	TxDirectionTransferred = txhelper.TxDirectionTransferred

	TxTypeRegular        = txhelper.TxTypeRegular
	TxTypeCoinBase       = txhelper.TxTypeCoinBase
	TxTypeTicketPurchase = txhelper.TxTypeTicketPurchase
	TxTypeVote           = txhelper.TxTypeVote
	TxTypeRevocation     = txhelper.TxTypeRevocation
)

func (lw *LibWallet) GetTransaction(txHash []byte) (string, error) {
	transaction, err := lw.GetTransactionRaw(txHash)
	if err != nil {
		log.Error(err)
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

	txSummary, _, blockHash, err := lw.wallet.TransactionSummary(hash)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	return lw.decodeTransactionWithTxSummary(txSummary, blockHash)
}

func (lw *LibWallet) GetTransactions(offset, limit, txFilter int32) (string, error) {
	var transactions []Transaction
	err := lw.txDB.Read(offset, limit, txFilter, &transactions)
	if err != nil {
		return "", err
	}

	jsonEncodedTransactions, err := json.Marshal(&transactions)
	if err != nil {
		return "", err
	}

	return string(jsonEncodedTransactions), nil
}

func (lw *LibWallet) CountTransactions(txFilter int32) (int, error) {
	return lw.txDB.Count(txFilter, &Transaction{})
}

func (lw *LibWallet) DetermineTxFilter(txType string, txDirection int32) int32 {
	return txindex.DetermineTxFilter(txType, txDirection)
}

//
//func (lw *LibWallet) parseTxSummary(tx *wallet.TransactionSummary, blockHash *chainhash.Hash) (*Transaction, error) {
//	var inputTotal int64
//	var outputTotal int64
//	var amount int64
//
//	credits := make([]TransactionCredit, len(tx.MyOutputs))
//	for index, credit := range tx.MyOutputs {
//		outputTotal += int64(credit.Amount)
//		credits[index] = TransactionCredit{
//			Index:    int32(credit.Index),
//			Account:  int32(credit.Account),
//			Internal: credit.Internal,
//			Amount:   int64(credit.Amount),
//			Address:  credit.Address.String()}
//	}
//
//	debits := make([]TransactionDebit, len(tx.MyInputs))
//	for index, debit := range tx.MyInputs {
//		inputTotal += int64(debit.PreviousAmount)
//		debits[index] = TransactionDebit{
//			Index:           int32(debit.Index),
//			PreviousAccount: int32(debit.PreviousAccount),
//			PreviousAmount:  int64(debit.PreviousAmount),
//			AccountName:     lw.AccountName(int32(debit.PreviousAccount))}
//	}
//
//	var direction = TxDirectionInvalid
//	if tx.Type == wallet.TransactionTypeRegular {
//		amountDifference := outputTotal - inputTotal
//		if amountDifference < 0 && (float64(tx.Fee) == math.Abs(float64(amountDifference))) {
//			//Transfered
//			direction = TxDirectionTransferred
//			amount = int64(tx.Fee)
//		} else if amountDifference > 0 {
//			//Received
//			direction = TxDirectionReceived
//			amount = outputTotal
//		} else {
//			//Sent
//			direction = TxDirectionSent
//			amount = inputTotal
//			amount -= outputTotal
//
//			amount -= int64(tx.Fee)
//		}
//	}
//
//	var height int32 = -1
//	if blockHash != nil {
//		blockIdentifier := wallet.NewBlockIdentifierFromHash(blockHash)
//		blockInfo, err := lw.wallet.BlockInfo(blockIdentifier)
//		if err != nil {
//			log.Error(err)
//		} else {
//			height = blockInfo.Height
//		}
//	}
//
//	transaction := &Transaction{
//		Fee:       int64(tx.Fee),
//		Hash:      fmt.Sprintf("%02x", reverse(tx.Hash[:])),
//		Raw:       fmt.Sprintf("%02x", tx.Transaction[:]),
//		Timestamp: tx.Timestamp,
//		Type:      transactionType(tx.Type),
//		Credits:   &credits,
//		Amount:    amount,
//		Height:    height,
//		Direction: direction,
//		Debits:    &debits}
//
//	return transaction, nil
//
//}
//
//func (lw *LibWallet) DecodeTransaction(txHash []byte) (string, error) {
//	hash, err := chainhash.NewHash(txHash)
//	if err != nil {
//		log.Error(err)
//		return "", err
//	}
//	txSummary, _, _, err := lw.wallet.TransactionSummary(hash)
//	if err != nil {
//		log.Error(err)
//		return "", err
//	}
//	serializedTx := txSummary.Transaction
//	var mtx wire.MsgTx
//	err = mtx.Deserialize(bytes.NewReader(serializedTx))
//	if err != nil {
//		log.Error(err)
//		return "", err
//	}
//
//	var ssGenVersion uint32
//	var lastBlockValid bool
//	var votebits string
//	if stake.IsSSGen(&mtx) {
//		ssGenVersion = voteVersion(&mtx)
//		lastBlockValid = voteBits(&mtx)&uint16(BlockValid) != 0
//		votebits = fmt.Sprintf("%#04x", voteBits(&mtx))
//	}
//
//	var tx = DecodedTransaction{
//		Hash:           fmt.Sprintf("%02x", reverse(hash[:])),
//		Type:           transactionType(wallet.TxTransactionType(&mtx)),
//		Version:        int32(mtx.Version),
//		LockTime:       int32(mtx.LockTime),
//		Expiry:         int32(mtx.Expiry),
//		Inputs:         decodeTxInputs(&mtx),
//		Outputs:        decodeTxOutputs(&mtx, lw.wallet.ChainParams()),
//		VoteVersion:    int32(ssGenVersion),
//		LastBlockValid: lastBlockValid,
//		VoteBits:       votebits,
//	}
//	result, _ := json.Marshal(tx)
//	return string(result), nil
//}
//
//// - Helper Functions
//
//func decodeTxInputs(mtx *wire.MsgTx) []DecodedInput {
//	inputs := make([]DecodedInput, len(mtx.TxIn))
//	for i, txIn := range mtx.TxIn {
//		inputs[i] = DecodedInput{
//			PreviousTransactionHash:  fmt.Sprintf("%02x", reverse(txIn.PreviousOutPoint.Hash[:])),
//			PreviousTransactionIndex: int32(txIn.PreviousOutPoint.Index),
//			AmountIn:                 txIn.ValueIn,
//		}
//	}
//	return inputs
//}
//
//func decodeTxOutputs(mtx *wire.MsgTx, chainParams *chaincfg.Params) []DecodedOutput {
//	outputs := make([]DecodedOutput, len(mtx.TxOut))
//	txType := stake.DetermineTxType(mtx)
//	for i, v := range mtx.TxOut {
//
//		var addrs []dcrutil.Address
//		var encodedAddrs []string
//		var scriptClass txscript.ScriptClass
//		if (txType == stake.TxTypeSStx) && (stake.IsStakeSubmissionTxOut(i)) {
//			scriptClass = txscript.StakeSubmissionTy
//			addr, err := stake.AddrFromSStxPkScrCommitment(v.PkScript,
//				chainParams)
//			if err != nil {
//				encodedAddrs = []string{fmt.Sprintf(
//					"[error] failed to decode ticket "+
//						"commitment addr output for tx hash "+
//						"%v, output idx %v", mtx.TxHash(), i)}
//			} else {
//				encodedAddrs = []string{addr.EncodeAddress()}
//			}
//		} else {
//			// Ignore the error here since an error means the script
//			// couldn't parse and there is no additional information
//			// about it anyways.
//			scriptClass, addrs, _, _ = txscript.ExtractPkScriptAddrs(
//				v.Version, v.PkScript, chainParams)
//			encodedAddrs = make([]string, len(addrs))
//			for j, addr := range addrs {
//				encodedAddrs[j] = addr.EncodeAddress()
//			}
//		}
//
//		outputs[i] = DecodedOutput{
//			Index:      int32(i),
//			Value:      v.Value,
//			Version:    int32(v.Version),
//			Addresses:  encodedAddrs,
//			ScriptType: scriptClass.String(),
//		}
//	}
//
//	return outputs
//}
//
//func voteVersion(mtx *wire.MsgTx) uint32 {
//	if len(mtx.TxOut[1].PkScript) < 8 {
//		return 0 // Consensus version absent
//	}
//
//	return binary.LittleEndian.Uint32(mtx.TxOut[1].PkScript[4:8])
//}
//
//func voteBits(mtx *wire.MsgTx) uint16 {
//	return binary.LittleEndian.Uint16(mtx.TxOut[1].PkScript[2:4])
//}
//
//func reverse(hash []byte) []byte {
//	for i := 0; i < len(hash)/2; i++ {
//		j := len(hash) - i - 1
//		hash[i], hash[j] = hash[j], hash[i]
//	}
//	return hash
//}
//
//func transactionType(txType wallet.TransactionType) string {
//	switch txType {
//	case wallet.TransactionTypeCoinbase:
//		return TxTypeCoinBase
//	case wallet.TransactionTypeTicketPurchase:
//		return TxTypeTicketPurchase
//	case wallet.TransactionTypeVote:
//		return TxTypeVote
//	case wallet.TransactionTypeRevocation:
//		return TxTypeRevocation
//	default:
//		return TxTypeRegular
//	}
//}
