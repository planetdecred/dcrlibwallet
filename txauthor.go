package dcrlibwallet

import (
	"bytes"
	"context"
	"fmt"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/raedahgroup/dcrlibwallet/addresshelper"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
	"time"
)

func (lw *LibWallet) ConstructTransaction(destAddr string, amount int64, srcAccount int32, requiredConfirmations int32, sendAll bool) (*UnsignedTransaction, error) {
	// output destination
	pkScript, err := addresshelper.PkScript(destAddr)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	version := txscript.DefaultScriptVersion

	// pay output
	outputs := make([]*wire.TxOut, 0)
	var algo wallet.OutputSelectionAlgorithm = wallet.OutputSelectionAlgorithmAll
	var changeSource txauthor.ChangeSource
	if !sendAll {
		algo = wallet.OutputSelectionAlgorithmDefault
		output := &wire.TxOut{
			Value:    amount,
			Version:  version,
			PkScript: pkScript,
		}
		outputs = append(outputs, output)
	} else {
		changeSource, err = txhelper.MakeTxChangeSource(destAddr)
		if err != nil {
			log.Error(err)
			return nil, err
		}
	}
	feePerKb := txrules.DefaultRelayFeePerKb

	// create tx
	tx, err := lw.wallet.NewUnsignedTransaction(outputs, feePerKb, uint32(srcAccount),
		requiredConfirmations, algo, changeSource)
	if err != nil {
		log.Error(err)
		return nil, translateError(err)
	}

	if tx.ChangeIndex >= 0 {
		tx.RandomizeChangePosition()
	}

	var txBuf bytes.Buffer
	txBuf.Grow(tx.Tx.SerializeSize())
	err = tx.Tx.Serialize(&txBuf)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	var totalOutput dcrutil.Amount
	for _, txOut := range outputs {
		totalOutput += dcrutil.Amount(txOut.Value)
	}

	return &UnsignedTransaction{
		UnsignedTransaction:       txBuf.Bytes(),
		TotalOutputAmount:         int64(totalOutput),
		TotalPreviousOutputAmount: int64(tx.TotalInput),
		EstimatedSignedSize:       tx.EstimatedSignedSerializeSize,
		ChangeIndex:               tx.ChangeIndex,
	}, nil
}

func (lw *LibWallet) CalculateNewTxFeeAndSize(amount int64, fromAccount int32, toAddress string, requiredConfirmations int32,
	spendAllFundsInAccount bool) (*TxFeeAndSize, error) {

	unsignedTx, err := lw.constructTransaction(amount, fromAccount, toAddress, requiredConfirmations, spendAllFundsInAccount)
	if err != nil {
		return nil, translateError(err)
	}

	feeToSendTx := txrules.FeeForSerializeSize(txrules.DefaultRelayFeePerKb, unsignedTx.EstimatedSignedSerializeSize)
	feeAmount := &Amount{
		AtomValue: int64(feeToSendTx),
		DcrValue:  feeToSendTx.ToCoin(),
	}

	return &TxFeeAndSize{
		EstimatedSignedSize: unsignedTx.EstimatedSignedSerializeSize,
		Fee:                 feeAmount,
	}, nil
}

func (lw *LibWallet) constructTransaction(amount int64, fromAccount int32, toAddress string, requiredConfirmations int32,
	spendAllFundsInAccount bool) (unsignedTx *txauthor.AuthoredTx, err error) {

	if !spendAllFundsInAccount && (amount <= 0 || amount > MaxAmountAtom) {
		return nil, errors.E(errors.Invalid, "invalid amount")
	}

	// `outputSelectionAlgorithm` specifies the algorithm to use when selecting outputs to construct a transaction.
	// If spendAllFundsInAccount == true, `outputSelectionAlgorithm` will be `wallet.OutputSelectionAlgorithmAll`.
	// Else, the default algorithm (`wallet.OutputSelectionAlgorithmDefault`) will be used.
	var outputSelectionAlgorithm wallet.OutputSelectionAlgorithm

	// If spendAllFundsInAccount == false, `outputs` will contain destination address and amount to send.
	// Else, the destination address will be used to make a `changeSource`.
	var outputs []*wire.TxOut
	var changeSource txauthor.ChangeSource

	if spendAllFundsInAccount {
		outputSelectionAlgorithm = wallet.OutputSelectionAlgorithmAll
		changeSource, err = txhelper.MakeTxChangeSource(toAddress)
	} else {
		outputSelectionAlgorithm = wallet.OutputSelectionAlgorithmDefault
		outputs, err = txhelper.MakeTxOutputs([]txhelper.TransactionDestination{
			{Address: toAddress, Amount: dcrutil.Amount(amount).ToCoin()},
		})
	}

	if err != nil {
		log.Error(err)
		return
	}

	return lw.wallet.NewUnsignedTransaction(outputs, txrules.DefaultRelayFeePerKb, uint32(fromAccount),
		requiredConfirmations, outputSelectionAlgorithm, changeSource)
}

func (lw *LibWallet) SendTransaction(privPass []byte, destAddr string, amount int64, srcAccount int32, requiredConfs int32, sendAll bool) ([]byte, error) {
	// output destination
	pkScript, err := addresshelper.PkScript(destAddr)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	// pay output
	outputs := make([]*wire.TxOut, 0)
	var algo wallet.OutputSelectionAlgorithm = wallet.OutputSelectionAlgorithmAll
	var changeSource txauthor.ChangeSource
	if !sendAll {
		algo = wallet.OutputSelectionAlgorithmDefault
		output := &wire.TxOut{
			Value:    amount,
			Version:  txscript.DefaultScriptVersion,
			PkScript: pkScript,
		}
		outputs = append(outputs, output)
	} else {
		changeSource, err = txhelper.MakeTxChangeSource(destAddr)
		if err != nil {
			log.Error(err)
			return nil, err
		}
	}

	// create tx
	unsignedTx, err := lw.wallet.NewUnsignedTransaction(outputs, txrules.DefaultRelayFeePerKb, uint32(srcAccount),
		requiredConfs, algo, changeSource)
	if err != nil {
		log.Error(err)
		return nil, translateError(err)
	}

	if unsignedTx.ChangeIndex >= 0 {
		unsignedTx.RandomizeChangePosition()
	}

	var txBuf bytes.Buffer
	txBuf.Grow(unsignedTx.Tx.SerializeSize())
	err = unsignedTx.Tx.Serialize(&txBuf)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	return lw.SignAndPublishTransaction(txBuf.Bytes(), privPass)
}

func (lw *LibWallet) BulkSendTransaction(privPass []byte, destinations []txhelper.TransactionDestination, srcAccount int32, requiredConfs int32) ([]byte, error) {
	nOutputs := len(destinations)
	var maxAmountRecipientAddress string
	for _, destination := range destinations {
		if destination.SendMax && maxAmountRecipientAddress != "" {
			return nil, fmt.Errorf("cannot send max amount to multiple recipients")
		} else if destination.SendMax {
			maxAmountRecipientAddress = destination.Address
			nOutputs--
		}
	}

	// create transaction outputs for all destination addresses and amounts, excluding destination for send max
	outputs := make([]*wire.TxOut, 0, nOutputs)
	for _, destination := range destinations {
		if !destination.SendMax {
			output, err := txhelper.MakeTxOutput(destination)
			if err != nil {
				log.Error(err)
				return nil, err
			}

			outputs = append(outputs, output)
		}
	}

	var algo wallet.OutputSelectionAlgorithm
	var changeSource txauthor.ChangeSource
	var err error

	// if no max amount recipient, use default utxo selection algorithm and nil change source
	// so that a change source to the sending account is automatically created
	// otherwise, create a change source for the max amount recipient so that the remaining change from the tx is sent to the max amount recipient
	if maxAmountRecipientAddress != "" {
		algo = wallet.OutputSelectionAlgorithmAll
		changeSource, err = txhelper.MakeTxChangeSource(maxAmountRecipientAddress)
		if err != nil {
			log.Error(err)
			return nil, err
		}
	} else {
		algo = wallet.OutputSelectionAlgorithmDefault
	}

	unsignedTx, err := lw.wallet.NewUnsignedTransaction(outputs, txrules.DefaultRelayFeePerKb, uint32(srcAccount), requiredConfs, algo, changeSource)
	if err != nil {
		log.Error(err)
		return nil, translateError(err)
	}

	if unsignedTx.ChangeIndex >= 0 {
		unsignedTx.RandomizeChangePosition()
	}

	var txBuf bytes.Buffer
	txBuf.Grow(unsignedTx.Tx.SerializeSize())
	err = unsignedTx.Tx.Serialize(&txBuf)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	return lw.SignAndPublishTransaction(txBuf.Bytes(), privPass)
}

func (lw *LibWallet) SendFromCustomInputs(sourceAccount uint32, requiredConfirmations int32, utxoKeys []string,
	txDestinations []txhelper.TransactionDestination, changeDestinations []txhelper.TransactionDestination, privPass []byte) (string, error) {

	// fetch all utxos in account to extract details for the utxos selected by user
	// use targetAmount = 0 to fetch ALL utxos in account
	unspentOutputs, err := lw.UnspentOutputs(sourceAccount, requiredConfirmations, 0)
	if err != nil {
		return "", err
	}

	// loop through unspentOutputs to find user selected utxos
	inputs := make([]*wire.TxIn, 0, len(utxoKeys))
	var totalInputAmount int64
	for _, utxo := range unspentOutputs {
		useUtxo := false
		for _, key := range utxoKeys {
			if utxo.OutputKey == key {
				useUtxo = true
			}
		}
		if !useUtxo {
			continue
		}

		// this is a reverse conversion and should not throw an error
		// this []byte was originally converted from chainhash.Hash using chainhash.Hash[:]
		txHash, _ := chainhash.NewHash(utxo.TransactionHash)

		outpoint := wire.NewOutPoint(txHash, utxo.OutputIndex, int8(utxo.Tree))
		input := wire.NewTxIn(outpoint, int64(utxo.Amount), nil)
		inputs = append(inputs, input)
		totalInputAmount += input.ValueIn

		if len(inputs) == len(utxoKeys) {
			break
		}
	}

	unsignedTx, err := txhelper.NewUnsignedTx(inputs, txDestinations, changeDestinations, func() (address string, err error) {
		return lw.NextAddress(int32(sourceAccount))
	})
	if err != nil {
		return "", err
	}

	// serialize unsigned tx
	var txBuf bytes.Buffer
	txBuf.Grow(unsignedTx.SerializeSize())
	err = unsignedTx.Serialize(&txBuf)
	if err != nil {
		return "", fmt.Errorf("error serializing transaction: %s", err.Error())
	}

	txHash, err := lw.SignAndPublishTransaction(txBuf.Bytes(), privPass)
	if err != nil {
		return "", err
	}

	transactionHash, err := chainhash.NewHash(txHash)
	if err != nil {
		return "", fmt.Errorf("error parsing successful transaction hash: %s", err.Error())
	}

	return transactionHash.String(), nil
}

func (lw *LibWallet) SignAndPublishTransaction(serializedTx, privPass []byte) ([]byte, error) {
	n, err := lw.wallet.NetworkBackend()
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer func() {
		for i := range privPass {
			privPass[i] = 0
		}
	}()

	var tx wire.MsgTx
	err = tx.Deserialize(bytes.NewReader(serializedTx))
	if err != nil {
		log.Error(err)
		//Bytes do not represent a valid raw transaction
		return nil, err
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{}
	}()

	err = lw.wallet.Unlock(privPass, lock)
	if err != nil {
		log.Error(err)
		return nil, errors.New(ErrInvalidPassphrase)
	}

	var additionalPkScripts map[wire.OutPoint][]byte

	invalidSigs, err := lw.wallet.SignTransaction(&tx, txscript.SigHashAll, additionalPkScripts, nil, nil)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	invalidInputIndexes := make([]uint32, len(invalidSigs))
	for i, e := range invalidSigs {
		invalidInputIndexes[i] = e.InputIndex
	}

	var serializedTransaction bytes.Buffer
	serializedTransaction.Grow(tx.SerializeSize())
	err = tx.Serialize(&serializedTransaction)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	var msgTx wire.MsgTx
	err = msgTx.Deserialize(bytes.NewReader(serializedTransaction.Bytes()))
	if err != nil {
		//Invalid tx
		log.Error(err)
		return nil, err
	}

	txHash, err := lw.wallet.PublishTransaction(&msgTx, serializedTransaction.Bytes(), n)
	if err != nil {
		return nil, translateError(err)
	}
	return txHash[:], nil
}

func (lw *LibWallet) PublishUnminedTransactions() error {
	netBackend, err := lw.wallet.NetworkBackend()
	if err != nil {
		return errors.New(ErrNotConnected)
	}
	ctx, _ := contextWithShutdownCancel(context.Background())
	err = lw.wallet.PublishUnminedTransactions(ctx, netBackend)
	return err
}
