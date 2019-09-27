package dcrlibwallet

import (
	"bytes"
	"fmt"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/raedahgroup/dcrlibwallet/txhelper"
)

type TxAuthor struct {
	lw                    *LibWallet
	sendFromAccount       uint32
	requiredConfirmations int32
	inputs                []*wire.TxIn
	destinations          []txhelper.TransactionDestination
	changeDestinations    []txhelper.TransactionDestination
}

func (lw *LibWallet) NewUnsignedTx(sourceAccountNumber, requiredConfirmations int32) *TxAuthor {
	return &TxAuthor{
		sendFromAccount:       uint32(sourceAccountNumber),
		destinations:          make([]txhelper.TransactionDestination, 0),
		requiredConfirmations: requiredConfirmations,
		lw:                    lw,
	}
}

func (tx *TxAuthor) SetSourceAccount(accountNumber int32) {
	tx.sendFromAccount = uint32(accountNumber)
}

func (tx *TxAuthor) AddSendDestination(address string, atomAmount int64, sendMax bool) {
	tx.destinations = append(tx.destinations, txhelper.TransactionDestination{
		Address:    address,
		AtomAmount: atomAmount,
		SendMax:    sendMax,
	})
}

func (tx *TxAuthor) UpdateSendDestination(index int, address string, atomAmount int64, sendMax bool) {
	tx.destinations[index] = txhelper.TransactionDestination{
		Address:    address,
		AtomAmount: atomAmount,
		SendMax:    sendMax,
	}
}

func (tx *TxAuthor) RemoveSendDestination(index int) {
	if len(tx.destinations) > index {
		tx.destinations = append(tx.destinations[:index], tx.destinations[index+1:]...)
	}
}

func (tx *TxAuthor) AddChangeDestination(address string, atomAmount int64) {
	tx.changeDestinations = append(tx.changeDestinations, txhelper.TransactionDestination{
		Address:    address,
		AtomAmount: atomAmount,
	})
}

func (tx *TxAuthor) UpdateChangeDestination(index int, address string, atomAmount int64) {
	tx.changeDestinations[index] = txhelper.TransactionDestination{
		Address:    address,
		AtomAmount: atomAmount,
	}
}

func (tx *TxAuthor) RemoveChangeDestination(index int) {
	if len(tx.changeDestinations) > index {
		tx.changeDestinations = append(tx.changeDestinations[:index], tx.changeDestinations[index+1:]...)
	}
}

func (tx *TxAuthor) EstimateFeeAndSize() (*TxFeeAndSize, error) {
	_, txSerializeSize, err := tx.constructTransaction()
	if err != nil {
		return nil, translateError(err)
	}

	feeToSendTx := txrules.FeeForSerializeSize(txrules.DefaultRelayFeePerKb, txSerializeSize)
	feeAmount := &Amount{
		AtomValue: int64(feeToSendTx),
		DcrValue:  feeToSendTx.ToCoin(),
	}

	return &TxFeeAndSize{
		EstimatedSignedSize: txSerializeSize,
		Fee:                 feeAmount,
	}, nil
}

func (tx *TxAuthor) EstimateMaxSendAmount() (*Amount, error) {
	sendMaxDestinations := 0
	for _, destination := range tx.destinations {
		if destination.SendMax {
			sendMaxDestinations++
		}
	}

	if sendMaxDestinations == 0 {
		return nil, fmt.Errorf("specify a send max destination to get max send amount estimate")
	} else if sendMaxDestinations > 1 {
		return nil, fmt.Errorf("multiple send max destinations not permitted")
	}

	txFeeAndSize, err := tx.EstimateFeeAndSize()
	if err != nil {
		return nil, err
	}

	spendableAccountBalance, err := tx.lw.SpendableForAccount(int32(tx.sendFromAccount), tx.requiredConfirmations)
	if err != nil {
		return nil, err
	}

	maxSendableAmount := spendableAccountBalance - txFeeAndSize.Fee.AtomValue

	return &Amount{
		AtomValue: maxSendableAmount,
		DcrValue:  dcrutil.Amount(maxSendableAmount).ToCoin(),
	}, nil
}

func (tx *TxAuthor) UseInputs(utxoKeys []string) error {
	// first clear any previously set inputs
	// so that an outdated set of inputs isn't used if an error occurs from this function
	tx.inputs = nil

	accountUtxos, err := tx.lw.AllUnspentOutputs(tx.sendFromAccount, tx.requiredConfirmations)
	if err != nil {
		return fmt.Errorf("error reading unspent outputs in account: %v", err)
	}

	retrieveAccountUtxo := func(utxoKey string) *UnspentOutput {
		for _, accountUtxo := range accountUtxos {
			if accountUtxo.OutputKey == utxoKey {
				return accountUtxo
			}
		}
		return nil
	}

	inputs := make([]*wire.TxIn, 0, len(utxoKeys))

	// retrieve utxo details for each key in the provided slice of utxoKeys
	for _, utxoKey := range utxoKeys {
		utxo := retrieveAccountUtxo(utxoKey)
		if utxo == nil {
			return fmt.Errorf("no valid utxo found for '%s' in the source account", utxoKey)
		}

		// this is a reverse conversion and should not throw an error
		// this []byte was originally converted from chainhash.Hash using chainhash.Hash[:]
		txHash, _ := chainhash.NewHash(utxo.TransactionHash)

		outpoint := wire.NewOutPoint(txHash, utxo.OutputIndex, int8(utxo.Tree))
		input := wire.NewTxIn(outpoint, int64(utxo.Amount), nil)
		inputs = append(inputs, input)
	}

	tx.inputs = inputs
	return nil
}

func (tx *TxAuthor) Broadcast(privatePassphrase []byte) (string, error) {
	defer func() {
		for i := range privatePassphrase {
			privatePassphrase[i] = 0
		}
	}()

	n, err := tx.lw.wallet.NetworkBackend()
	if err != nil {
		log.Error(err)
		return "", err
	}

	unsignedTx, _, err := tx.constructTransaction()
	if err != nil {
		return "", translateError(err)
	}

	var txBuf bytes.Buffer
	txBuf.Grow(unsignedTx.SerializeSize())
	err = unsignedTx.Serialize(&txBuf)
	if err != nil {
		log.Error(err)
		return "", err
	}

	var msgTx wire.MsgTx
	err = msgTx.Deserialize(bytes.NewReader(txBuf.Bytes()))
	if err != nil {
		log.Error(err)
		//Bytes do not represent a valid raw transaction
		return "", err
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{}
	}()

	err = tx.lw.wallet.Unlock(privatePassphrase, lock)
	if err != nil {
		log.Error(err)
		return "", errors.New(ErrInvalidPassphrase)
	}

	var additionalPkScripts map[wire.OutPoint][]byte

	invalidSigs, err := tx.lw.wallet.SignTransaction(&msgTx, txscript.SigHashAll, additionalPkScripts, nil, nil)
	if err != nil {
		log.Error(err)
		return "", err
	}

	invalidInputIndexes := make([]uint32, len(invalidSigs))
	for i, e := range invalidSigs {
		invalidInputIndexes[i] = e.InputIndex
	}

	var serializedTransaction bytes.Buffer
	serializedTransaction.Grow(msgTx.SerializeSize())
	err = msgTx.Serialize(&serializedTransaction)
	if err != nil {
		log.Error(err)
		return "", err
	}

	err = msgTx.Deserialize(bytes.NewReader(serializedTransaction.Bytes()))
	if err != nil {
		//Invalid tx
		log.Error(err)
		return "", err
	}

	txHash, err := tx.lw.wallet.PublishTransaction(&msgTx, serializedTransaction.Bytes(), n)
	if err != nil {
		return "", translateError(err)
	}

	return txHash.String(), nil
}

func (tx *TxAuthor) constructTransaction() (*wire.MsgTx, int, error) {
	if len(tx.inputs) != 0 {
		// custom transaction
		return tx.constructCustomTransaction()
	}

	if len(tx.changeDestinations) != 0 {
		return nil, 0, fmt.Errorf("specify custom inputs to use custom change outputs")
	}

	outputs, _, maxAmountRecipientAddress, err := txhelper.ParseOutputsAndChangeDestination(tx.destinations)
	if err != nil {
		log.Errorf("constructTransaction: error parsing tx destinations: %v", err)
		return nil, 0, fmt.Errorf("error parsing tx destinations: %v", err)
	}

	var outputSelectionAlgorithm wallet.OutputSelectionAlgorithm = wallet.OutputSelectionAlgorithmDefault
	var changeSource txauthor.ChangeSource

	if maxAmountRecipientAddress != "" {
		// set output selection algo to all
		outputSelectionAlgorithm = wallet.OutputSelectionAlgorithmAll
		// use the address to make a changeSource
		changeSource, err = txhelper.MakeTxChangeSource(maxAmountRecipientAddress)
		if err != nil {
			log.Errorf("constructTransaction: error preparing change source: %v", err)
			return nil, 0, fmt.Errorf("max amount change source error: %v", err)
		}
	}

	unsignedTx, err := tx.lw.wallet.NewUnsignedTransaction(outputs, txrules.DefaultRelayFeePerKb, tx.sendFromAccount,
		tx.requiredConfirmations, outputSelectionAlgorithm, changeSource)
	if err != nil {
		return nil, 0, err
	}

	return unsignedTx.Tx, unsignedTx.EstimatedSignedSerializeSize, nil
}

func (tx *TxAuthor) constructCustomTransaction() (*wire.MsgTx, int, error) {
	// Used to generate an internal address for change,
	// if no change destination is provided and
	// no recipient is set to receive max amount.
	changeAddressFn := func() (string, error) {
		addr, err := tx.lw.wallet.NewChangeAddress(tx.sendFromAccount)
		if err != nil {
			return "", err
		}
		return addr.EncodeAddress(), nil
	}

	return txhelper.NewUnsignedTx(tx.inputs, tx.destinations, tx.changeDestinations, changeAddressFn)
}
