package dcrlibwallet

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrwallet/errors/v2"
)

// GetAccounts returns a json array of all
// accounts information in a wallet.
func (wallet *Wallet) GetAccounts() (string, error) {
	accountsResponse, err := wallet.GetAccountsRaw()
	if err != nil {
		return "", nil
	}

	result, _ := json.Marshal(accountsResponse)
	return string(result), nil
}

// GetAccountsRaw returns an Accounts pointer containing all accounts information in a wallet.
func (wallet *Wallet) GetAccountsRaw() (*Accounts, error) {
	resp, err := wallet.internal.Accounts(wallet.shutdownContext())
	if err != nil {
		return nil, err
	}
	accounts := make([]*Account, len(resp.Accounts))
	for i, account := range resp.Accounts {
		balance, err := wallet.GetAccountBalance(int32(account.AccountNumber))
		if err != nil {
			return nil, err
		}

		accounts[i] = &Account{
			WalletID:         wallet.ID,
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

// AccountsIterator returns an iterator that can be
// used to loop through the accounts of a wallet.
func (wallet *Wallet) AccountsIterator() (*AccountsIterator, error) {
	accounts, err := wallet.GetAccountsRaw()
	if err != nil {
		return nil, err
	}

	return &AccountsIterator{
		currentIndex: 0,
		accounts:     accounts.Acc,
	}, nil
}

// Next returns the account on the current iterator
// index and increments the current index.
func (accountsInterator *AccountsIterator) Next() *Account {
	if accountsInterator.currentIndex < len(accountsInterator.accounts) {
		account := accountsInterator.accounts[accountsInterator.currentIndex]
		accountsInterator.currentIndex++
		return account
	}

	return nil
}

// Reset sets the current iterator's index to 0.
func (accountsInterator *AccountsIterator) Reset() {
	accountsInterator.currentIndex = 0
}

// GetAccount fetches an account and all the accompanying
// information and properties of the said account.
func (wallet *Wallet) GetAccount(accountNumber int32) (*Account, error) {
	props, err := wallet.internal.AccountProperties(wallet.shutdownContext(), uint32(accountNumber))
	if err != nil {
		return nil, err
	}

	balance, err := wallet.GetAccountBalance(accountNumber)
	if err != nil {
		return nil, err
	}

	account := &Account{
		WalletID:         wallet.ID,
		Number:           accountNumber,
		Name:             props.AccountName,
		TotalBalance:     balance.Total,
		Balance:          balance,
		ExternalKeyCount: int32(props.LastUsedExternalIndex + 20),
		InternalKeyCount: int32(props.LastUsedInternalIndex + 20),
		ImportedKeyCount: int32(props.ImportedKeyCount),
	}

	return account, nil
}

// GetAccountBalance returns all the balance
// information in a given account.
func (wallet *Wallet) GetAccountBalance(accountNumber int32) (*Balance, error) {
	balance, err := wallet.internal.CalculateAccountBalance(wallet.shutdownContext(), uint32(accountNumber), wallet.RequiredConfirmations()
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

// SpendableForAccount returns the spendable
// balance in a given account.
func (wallet *Wallet) SpendableForAccount(account int32) (int64, error) {
	bals, err := wallet.internal.CalculateAccountBalance(wallet.shutdownContext(), uint32(account), wallet.RequiredConfirmations())
	if err != nil {
		log.Error(err)
		return 0, translateError(err)
	}
	return int64(bals.Spendable), nil
}

// NextAccount creates the account and returns the
// account number.
func (wallet *Wallet) NextAccount(accountName string, privPass []byte) (int32, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		for i := range privPass {
			privPass[i] = 0
		}
		lock <- time.Time{} // send matters, not the value
	}()

	ctx := wallet.shutdownContext()
	err := wallet.internal.Unlock(ctx, privPass, lock)
	if err != nil {
		log.Error(err)
		return 0, errors.New(ErrInvalidPassphrase)
	}

	accountNumber, err := wallet.internal.NextAccount(ctx, accountName)

	return int32(accountNumber), err
}

// RenameAccount sets the name for an account number to newName.
func (wallet *Wallet) RenameAccount(accountNumber int32, newName string) error {
	err := wallet.internal.RenameAccount(wallet.shutdownContext(), uint32(accountNumber), newName)
	if err != nil {
		return translateError(err)
	}

	return nil
}

// AccountName returns the name for the passed account number.
func (wallet *Wallet) AccountName(accountNumber int32) string {
	name, err := wallet.AccountNameRaw(uint32(accountNumber))
	if err != nil {
		log.Error(err)
		return "Account not found"
	}
	return name
}

// AccountNameRaw returns the name for the passed account number.
func (wallet *Wallet) AccountNameRaw(accountNumber uint32) (string, error) {
	return wallet.internal.AccountName(wallet.shutdownContext(), accountNumber)
}

// AccountNumber returns an account number for the passed account.
func (wallet *Wallet) AccountNumber(accountName string) (uint32, error) {
	return wallet.internal.AccountNumber(wallet.shutdownContext(), accountName)
}

// HDPathForAccount returns the HD path for the passed account based
// on the coin-type and the net type of the wallet.
func (wallet *Wallet) HDPathForAccount(accountNumber int32) (string, error) {
	cointype, err := wallet.internal.CoinType(wallet.shutdownContext())
	if err != nil {
		return "", translateError(err)
	}

	var hdPath string
	isLegacyCoinType := cointype == wallet.chainParams.LegacyCoinType
	if wallet.chainParams.Name == chaincfg.MainNetParams().Name {
		if isLegacyCoinType {
			hdPath = LegacyMainnetHDPath
		} else {
			hdPath = MainnetHDPath
		}
	} else {
		if isLegacyCoinType {
			hdPath = LegacyTestnetHDPath
		} else {
			hdPath = TestnetHDPath
		}
	}

	return hdPath + strconv.Itoa(int(accountNumber)), nil
}
