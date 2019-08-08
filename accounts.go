package dcrlibwallet

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet"
)

// GetAccounts returns a `Json encoded` representation of all accounts in
// a single wallet
func (lw *LibWallet) GetAccounts(requiredConfirmations int32) (string, error) {
	accountsResponse, err := lw.GetAccountsRaw(requiredConfirmations)
	if err != nil {
		return "", nil
	}

	result, _ := json.Marshal(accountsResponse)
	return string(result), nil
}

// GetAccountsRaw returns all accounts in a single wallet
func (lw *LibWallet) GetAccountsRaw(requiredConfirmations int32) (*Accounts, error) {
	resp, err := lw.wallet.Accounts()
	if err != nil {
		return nil, err
	}
	accounts := make([]*Account, len(resp.Accounts))
	for i, account := range resp.Accounts {
		balance, err := lw.GetAccountBalance(account.AccountNumber, requiredConfirmations)
		if err != nil {
			return nil, err
		}

		accounts[i] = &Account{
			Number:           int32(account.AccountNumber),
			Name:             account.AccountName,
			TotalBalance:     int64(account.TotalBalance),
			Balance:          balance,
			ExternalKeyCount: int32(account.LastUsedExternalIndex + 20),
			InternalKeyCount: int32(account.LastUsedInternalIndex + 20),
			ImportedKeyCount: int32(account.ImportedKeyCount),
		}
	}

	return &Accounts{
		Count:              len(resp.Accounts),
		CurrentBlockHash:   resp.CurrentBlockHash[:],
		CurrentBlockHeight: resp.CurrentBlockHeight,
		Acc:                accounts,
		ErrorOccurred:      false,
	}, nil
}

// GetAcountBalance returns a Balance type which represents
// the remaining balance in a given wallet account
func (lw *LibWallet) GetAccountBalance(accountNumber uint32, requiredConfirmations int32) (*Balance, error) {
	balance, err := lw.wallet.CalculateAccountBalance(accountNumber, requiredConfirmations)
	if err != nil {
		return nil, err
	}

	return &Balance{
		Total:                   int64(balance.Total),
		Spendable:               int64(balance.Spendable),
		ImmatureReward:          int64(balance.ImmatureCoinbaseRewards),
		ImmatureStakeGeneration: int64(balance.ImmatureStakeGeneration),
		LockedByTickets:         int64(balance.LockedByTickets),
		VotingAuthority:         int64(balance.VotingAuthority),
		UnConfirmed:             int64(balance.Unconfirmed),
	}, nil
}

// SpendableForAccount calculates the unspent transactions
// of a given wallet account and returns only the spendable balance
func (lw *LibWallet) SpendableForAccount(account int32, requiredConfirmations int32) (int64, error) {
	bals, err := lw.wallet.CalculateAccountBalance(uint32(account), requiredConfirmations)
	if err != nil {
		log.Error(err)
		return 0, err
	}
	return int64(bals.Spendable), nil
}

func (lw *LibWallet) UnspentOutputs(account uint32, requiredConfirmations int32, targetAmount int64) ([]*UnspentOutput, error) {
	policy := wallet.OutputSelectionPolicy{
		Account:               account,
		RequiredConfirmations: requiredConfirmations,
	}
	inputDetail, err := lw.wallet.SelectInputs(dcrutil.Amount(targetAmount), policy)
	// Do not return errors to caller when there was insufficient spendable
	// outputs available for the target amount.
	if err != nil && !errors.Is(errors.InsufficientBalance, err) {
		return nil, err
	}

	unspentOutputs := make([]*UnspentOutput, len(inputDetail.Inputs))

	for i, input := range inputDetail.Inputs {
		outputInfo, err := lw.wallet.OutputInfo(&input.PreviousOutPoint)
		if err != nil {
			return nil, err
		}

		// unique key to identify utxo
		outputKey := fmt.Sprintf("%s:%d", input.PreviousOutPoint.Hash, input.PreviousOutPoint.Index)

		unspentOutputs[i] = &UnspentOutput{
			TransactionHash: input.PreviousOutPoint.Hash[:],
			OutputIndex:     input.PreviousOutPoint.Index,
			OutputKey:       outputKey,
			Tree:            int32(input.PreviousOutPoint.Tree),
			Amount:          int64(outputInfo.Amount),
			PkScript:        inputDetail.Scripts[i],
			ReceiveTime:     outputInfo.Received.Unix(),
			FromCoinbase:    outputInfo.FromCoinbase,
		}
	}

	return unspentOutputs, nil
}

func (lw *LibWallet) NextAccount(accountName string, privPass []byte) error {
	_, err := lw.NextAccountRaw(accountName, privPass)
	if err != nil {
		log.Error(err)
		return err
	}
	return nil
}

func (lw *LibWallet) NextAccountRaw(accountName string, privPass []byte) (uint32, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		for i := range privPass {
			privPass[i] = 0
		}
		lock <- time.Time{} // send matters, not the value
	}()
	err := lw.wallet.Unlock(privPass, lock)
	if err != nil {
		log.Error(err)
		return 0, errors.New(ErrInvalidPassphrase)
	}

	return lw.wallet.NextAccount(accountName)
}

// RenameAccount sets the name for an account number to newName.
func (lw *LibWallet) RenameAccount(accountNumber int32, newName string) error {
	err := lw.wallet.RenameAccount(uint32(accountNumber), newName)
	return err
}

// AccountName takes in accountNumber as input and returns
// the name of the account if it exists
func (lw *LibWallet) AccountName(accountNumber int32) string {
	name, err := lw.AccountNameRaw(uint32(accountNumber))
	if err != nil {
		log.Error(err)
		return "Account not found"
	}
	return name
}

// AccountNameRaw returns the name of an account.
func (lw *LibWallet) AccountNameRaw(accountNumber uint32) (string, error) {
	return lw.wallet.AccountName(accountNumber)
}

// AccountNumber returns the account number for an account name.
func (lw *LibWallet) AccountNumber(accountName string) (uint32, error) {
	return lw.wallet.AccountNumber(accountName)
}

// CurrentAddress returns the most recently requested payment address
// from a wallet
func (lw *LibWallet) CurrentAddress(account int32) (string, error) {
	addr, err := lw.wallet.CurrentAddress(uint32(account))
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.EncodeAddress(), nil
}

func (lw *LibWallet) NextAddress(account int32) (string, error) {
	var callOpts []wallet.NextAddressCallOption
	callOpts = append(callOpts, wallet.WithGapPolicyWrap())

	addr, err := lw.wallet.NewExternalAddress(uint32(account), callOpts...)
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.EncodeAddress(), nil
}
