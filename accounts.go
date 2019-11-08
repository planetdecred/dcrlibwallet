package dcrlibwallet

import (
	"encoding/json"
	"time"

	"github.com/decred/dcrwallet/errors/v2"
)

func (lw *LibWallet) HasDiscoveredAccounts() bool {
	return lw.Properties.DiscoveredAccounts
}

func (lw *LibWallet) GetAccounts(requiredConfirmations int32) (string, error) {
	accountsResponse, err := lw.GetAccountsRaw(requiredConfirmations)
	if err != nil {
		return "", nil
	}

	result, _ := json.Marshal(accountsResponse)
	return string(result), nil
}

func (lw *LibWallet) GetAccountsRaw(requiredConfirmations int32) (*Accounts, error) {
	ctx := lw.shutdownContext()
	resp, err := lw.wallet.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	accounts := make([]*Account, len(resp.Accounts))
	for i, account := range resp.Accounts {
		balance, err := lw.GetAccountBalance(int32(account.AccountNumber), requiredConfirmations)
		if err != nil {
			return nil, err
		}

		accounts[i] = &Account{
			WalletID:         lw.ID,
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
	}, nil
}

func (lw *LibWallet) AccountsIterator(requiredConfirmations int32) (*AccountsIterator, error) {
	resp, err := lw.wallet.Accounts()
	if err != nil {
		return nil, err
	}
	accounts := make([]*Account, len(resp.Accounts))
	for i, account := range resp.Accounts {
		balance, err := lw.GetAccountBalance(int32(account.AccountNumber), requiredConfirmations)
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

	return &AccountsIterator{
		currentIndex: 0,
		accounts:     accounts,
	}, nil
}

func (accountsInterator *AccountsIterator) Next() *Account {
	if accountsInterator.currentIndex < len(accountsInterator.accounts) {
		account := accountsInterator.accounts[accountsInterator.currentIndex]
		accountsInterator.currentIndex++
		return account
	}

	return nil
}

func (accountsInterator *AccountsIterator) Reset() {
	accountsInterator.currentIndex = 0
}

func (lw *LibWallet) GetAccountBalance(accountNumber int32, requiredConfirmations int32) (*Balance, error) {
	ctx := lw.shutdownContext()
	balance, err := lw.wallet.CalculateAccountBalance(ctx, uint32(accountNumber), requiredConfirmations)
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

func (lw *LibWallet) SpendableForAccount(account int32, requiredConfirmations int32) (int64, error) {
	ctx := lw.shutdownContext()
	bals, err := lw.wallet.CalculateAccountBalance(ctx, uint32(account), requiredConfirmations)
	if err != nil {
		log.Error(err)
		return 0, translateError(err)
	}
	return int64(bals.Spendable), nil
}

func (lw *LibWallet) NextAccount(accountName string, privPass []byte) (int32, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		for i := range privPass {
			privPass[i] = 0
		}
		lock <- time.Time{} // send matters, not the value
	}()

	ctx := lw.shutdownContext()
	err := lw.wallet.Unlock(ctx, privPass, lock)
	if err != nil {
		log.Error(err)
		return 0, errors.New(ErrInvalidPassphrase)
	}

	accountNumber, err := lw.wallet.NextAccount(ctx, accountName)

	return int32(accountNumber), err
}

func (lw *LibWallet) RenameAccount(accountNumber int32, newName string) error {
	ctx := lw.shutdownContext()
	err := lw.wallet.RenameAccount(ctx, uint32(accountNumber), newName)
	if err != nil {
		return translateError(err)
	}

	return nil
}

func (lw *LibWallet) AccountName(accountNumber int32) string {
	name, err := lw.AccountNameRaw(uint32(accountNumber))
	if err != nil {
		log.Error(err)
		return "Account not found"
	}
	return name
}

func (lw *LibWallet) AccountNameRaw(accountNumber uint32) (string, error) {
	ctx := lw.shutdownContext()
	return lw.wallet.AccountName(ctx, accountNumber)
}

func (lw *LibWallet) AccountNumber(accountName string) (uint32, error) {
	ctx := lw.shutdownContext()
	return lw.wallet.AccountNumber(ctx, accountName)
}
