package dcrlibwallet

import (
	"fmt"

	w "decred.org/dcrwallet/v2/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

const BlockHeightInvalid int32 = -1

func (wallet *Wallet) decodeTransactionWithTxSummary(txSummary *w.TransactionSummary,
	blockHash *chainhash.Hash) (*Transaction, error) {

	var blockHeight int32 = BlockHeightInvalid
	if blockHash != nil {
		blockIdentifier := w.NewBlockIdentifierFromHash(blockHash)
		blockInfo, err := wallet.Internal().BlockInfo(wallet.shutdownContext(), blockIdentifier)
		if err != nil {
			log.Error(err)
		} else {
			blockHeight = blockInfo.Height
		}
	}

	walletInputs := make([]*WalletInput, len(txSummary.MyInputs))
	for i, input := range txSummary.MyInputs {
		accountNumber := int32(input.PreviousAccount)
		accountName, err := wallet.AccountName(accountNumber)
		if err != nil {
			log.Error(err)
		}

		walletInputs[i] = &WalletInput{
			Index:    int32(input.Index),
			AmountIn: int64(input.PreviousAmount),
			WalletAccount: &WalletAccount{
				AccountNumber: accountNumber,
				AccountName:   accountName,
			},
		}
	}

	walletOutputs := make([]*WalletOutput, len(txSummary.MyOutputs))
	for i, output := range txSummary.MyOutputs {
		accountNumber := int32(output.Account)
		accountName, err := wallet.AccountName(accountNumber)
		if err != nil {
			log.Error(err)
		}

		walletOutputs[i] = &WalletOutput{
			Index:     int32(output.Index),
			AmountOut: int64(output.Amount),
			Internal:  output.Internal,
			Address:   output.Address.String(),
			WalletAccount: &WalletAccount{
				AccountNumber: accountNumber,
				AccountName:   accountName,
			},
		}
	}

	walletTx := &TxInfoFromWallet{
		WalletID:    wallet.ID,
		BlockHeight: blockHeight,
		Timestamp:   txSummary.Timestamp,
		Hex:         fmt.Sprintf("%x", txSummary.Transaction),
		Inputs:      walletInputs,
		Outputs:     walletOutputs,
	}

	decodedTx, err := wallet.DecodeTransaction(walletTx, wallet.chainParams)
	if err != nil {
		return nil, err
	}

	if decodedTx.TicketSpentHash != "" {
		ticketPurchaseTx, err := wallet.GetTransactionRaw(decodedTx.TicketSpentHash)
		if err != nil {
			return nil, err
		}

		timeDifferenceInSeconds := decodedTx.Timestamp - ticketPurchaseTx.Timestamp
		decodedTx.DaysToVoteOrRevoke = int32(timeDifferenceInSeconds / 86400) // seconds to days conversion

		// calculate reward
		var ticketInvestment int64
		for _, input := range ticketPurchaseTx.Inputs {
			if input.AccountNumber > -1 {
				ticketInvestment += input.Amount
			}
		}

		var ticketOutput int64
		for _, output := range walletTx.Outputs {
			if output.AccountNumber > -1 {
				ticketOutput += output.AmountOut
			}
		}

		reward := ticketOutput - ticketInvestment
		decodedTx.VoteReward = reward

		// update ticket with spender hash
		ticketPurchaseTx.TicketSpender = decodedTx.Hash
		wallet.walletDataDB.SaveOrUpdate(&Transaction{}, ticketPurchaseTx)
	}

	return decodedTx, nil
}
