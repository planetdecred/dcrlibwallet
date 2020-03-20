package dcrlibwallet_test

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"strconv"
	"time"

	w "github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/raedahgroup/dcrlibwallet"
)

const rootDir = ".dcrlibwallet_tests_root"

func init() {
	SetLogLevels("error")
}

var _ = Describe("Account", func() {
	var (
		wallet         *Wallet
		internalWallet *w.Wallet
		password       string
	)

	BeforeSuite(func() {
		os.RemoveAll(rootDir)
		multi, err := NewMultiWallet(rootDir, "", "testnet3")
		Expect(err).To(BeNil())
		Expect(multi).ToNot(BeNil())

		password = randomPassword()
		wallet, err = multi.CreateNewWallet(password, 0)
		Expect(err).To(BeNil())
		Expect(wallet).ToNot(BeNil())
		internalWallet = wallet.InternalWallet()
	})

	AfterSuite(func() {
		if wallet != nil {
			wallet.Shutdown()
		}
	})

	getWrongAccountNumber := func() uint32 {
		var accountNumber uint32 = 1220
		var err error
		for err == nil {
			accountNumber++
			_, err = internalWallet.AccountName(context.Background(), accountNumber)
		}
		return accountNumber
	}

	assertBalanceEqual := func(intBal udb.Balances, bal *Balance) {
		Expect(bal.ImmatureReward).To(BeEquivalentTo(intBal.ImmatureCoinbaseRewards))
		Expect(bal.ImmatureStakeGeneration).To(BeEquivalentTo(intBal.ImmatureStakeGeneration))
		Expect(bal.LockedByTickets).To(BeEquivalentTo(intBal.LockedByTickets))
		Expect(bal.Spendable).To(BeEquivalentTo(intBal.Spendable))
		Expect(bal.Total).To(BeEquivalentTo(intBal.Total))
		Expect(bal.UnConfirmed).To(BeEquivalentTo(intBal.Unconfirmed))
		Expect(bal.VotingAuthority).To(BeEquivalentTo(intBal.VotingAuthority))
	}

	assertAccountsEquals := func(intAcc w.AccountResult, acc *Account) {
		Expect(intAcc.AccountName).To(BeEquivalentTo(acc.Name))
		Expect(intAcc.AccountNumber).To(BeEquivalentTo(acc.Number))
		Expect(intAcc.AccountNumber).To(BeEquivalentTo(acc.Number))
		Expect(int32(int32(intAcc.LastUsedExternalIndex + 20))).To(BeEquivalentTo(acc.ExternalKeyCount))
		Expect(int32(intAcc.LastUsedInternalIndex + 20)).To(BeEquivalentTo(acc.InternalKeyCount))
		Expect(intAcc.ImportedKeyCount).To(BeEquivalentTo(acc.ImportedKeyCount))
		intBalance, err := internalWallet.CalculateAccountBalance(context.Background(), uint32(acc.Number), 0)
		Expect(err).To(BeNil())
		assertBalanceEqual(intBalance, acc.Balance)
	}

	Describe("GetAccounts", func() {
		It("should get accounts", func() {
			res, err := wallet.GetAccounts(0)
			By("returning a nil error")
			Expect(err).To(BeNil())
			By("returning nonempty accounts string")
			Expect(res).ToNot(BeEquivalentTo(""))
			var accountsRes Accounts
			err = json.Unmarshal([]byte(res), &accountsRes)
			Expect(err).To(BeNil())
			internalAccount, err := internalWallet.Accounts(context.Background())
			Expect(err).To(BeNil())
			Expect(accountsRes.Count).To(BeEquivalentTo(len(internalAccount.Accounts)))
			Expect(accountsRes.CurrentBlockHash).To(BeEquivalentTo(internalAccount.CurrentBlockHash[:]))
			Expect(accountsRes.CurrentBlockHeight).To(BeEquivalentTo(internalAccount.CurrentBlockHeight))
			for i := 0; i < len(internalAccount.Accounts); i++ {
				assertAccountsEquals(internalAccount.Accounts[i], accountsRes.Acc[i])
			}
		})
	})

	Describe("GetAccount", func() {
		Context("when called with the right parameters", func() {
			It("should get account", func() {
				var accountNumber uint32 = 0
				account, err := wallet.GetAccount(int32(accountNumber), 0)
				By("returning a nil error")
				Expect(err).To(BeNil())
				By("returning a non nil account")
				Expect(account).ToNot(BeNil())
				By("returning an account with the supplied account number")
				Expect(account.Number).To(BeEquivalentTo(accountNumber))
				intAccount, err := internalWallet.AccountProperties(context.Background(), accountNumber)
				Expect(err).To(BeNil())
				assertAccountsEquals(w.AccountResult{
					AccountProperties: *intAccount,
				}, account)
			})
		})

		Context("when called for non-existing account", func() {
			It("it should fail", func() {
				account, err := wallet.GetAccount(1000000000, 0)
				By("returning a non nil error")
				Expect(err).ToNot(BeNil())
				By("by returning nil account")
				Expect(account).To(BeNil())
			})
		})
	})

	Describe("GetAccountBalance", func() {
		It("should get the account balance", func() {
			var accountNumber uint32 = 0
			var confirmations int32 = 0
			internalBalance, err := internalWallet.CalculateAccountBalance(context.Background(), accountNumber, confirmations)
			Expect(err).To(BeNil())
			balance, err := wallet.GetAccountBalance(int32(accountNumber), confirmations)
			By("returning a nil error")
			Expect(err).To(BeNil())
			By("returning the expected balance")
			Expect(balance).ToNot(BeNil())
			assertBalanceEqual(internalBalance, balance)
		})
	})

	Describe("SpendableForAccount", func() {
		It("should return the spendable balance", func() {
			var accountNumber uint32 = 0
			var confirmations int32 = 0
			internalBalance, err := internalWallet.CalculateAccountBalance(context.Background(), accountNumber, confirmations)
			Expect(err).To(BeNil())
			balance, err := wallet.SpendableForAccount(int32(accountNumber), confirmations)
			By("returning a nil error")
			Expect(err).To(BeNil())
			By("returning the expected balance")
			Expect(balance).To(BeEquivalentTo(internalBalance.Spendable))
		})
	})

	Describe("NextAccount", func() {
		Context("when called with the right args", func() {
			It("should return the next account number", func() {
				accountNumber, err := wallet.NextAccount("account 1", []byte(password))
				By("returning a nil error")
				Expect(err).To(BeNil())
				By("returning the expected account number")
				Expect(accountNumber).To(BeEquivalentTo(int32(1)))
			})
		})
		Context("when called with a wrong password", func() {
			It("should failed", func() {
				var wrongPasword = randomPassword()
				for wrongPasword == password {
					wrongPasword += "1"
				}
				_, err := wallet.NextAccount("account 1", []byte(wrongPasword))
				By("returning a non nil error")
				Expect(err).ToNot(BeNil())
				By("reporting invalid password")
				Expect(err.Error()).To(BeEquivalentTo("invalid_passphrase"))
			})
		})
	})

	Describe("RenameAccount", func() {
		Context("when called with the right args", func() {
			It("should rename the account", func() {
				updatedName := "first account"
				err := wallet.RenameAccount(0, updatedName)
				By("returning a nil error")
				Expect(err).To(BeNil())

				account, err := wallet.GetAccount(0, 0)
				Expect(err).To(BeNil())
				By("changing the account name in the wallet")
				Expect(account.Name).To(BeEquivalentTo(updatedName))
			})
		})
		Context("when called with a non-existing account number", func() {
			It("should fail", func() {
				err := wallet.RenameAccount(56, "first account")
				By("returning a non nil error")
				Expect(err).ToNot(BeNil())
			})
		})
	})

	Describe("AccountName", func() {
		Context("when called with an existing account number", func() {
			It("should return the account name", func() {
				var accountNumber uint32 = 0
				internalAccountName, err := internalWallet.AccountName(context.Background(), accountNumber)
				Expect(err).To(BeNil())
				name := wallet.AccountName(0)
				By("returning the expected account name")
				Expect(name).To(BeEquivalentTo(internalAccountName))
			})
		})

		Context("when called with a non-existing account number", func() {
			It("should fail", func() {
				var wrongAccountNumber uint32 = getWrongAccountNumber()
				name := wallet.AccountName(int32(wrongAccountNumber))
				By("returning 'Account not found'")
				Expect(name).To(BeEquivalentTo("Account not found"))
			})
		})
	})

	Describe("AccountNameRaw", func() {
		Context("when called with a valid account number", func() {
			It("should return the account name", func() {
				var accountNumber uint32 = 0
				internalAccountName, err := internalWallet.AccountName(context.Background(), accountNumber)
				Expect(err).To(BeNil())
				name, err := wallet.AccountNameRaw(accountNumber)
				By("returning a nil error")
				Expect(err).To(BeNil())
				By("returning the expected account name")
				Expect(name).To(BeEquivalentTo(internalAccountName))
			})
		})
		Context("when called with a non-existing account number", func() {
			It("should fail", func() {
				var wrongAccountNumber = getWrongAccountNumber()
				_, err := wallet.AccountNameRaw(wrongAccountNumber)
				By("returning a non nil error")
				Expect(err).ToNot(BeNil())
			})
		})
	})

	Describe("AccountNumber", func() {
		It("should return the account number", func() {
			var accountNumber uint32 = 0
			internalAccountName, err := internalWallet.AccountName(context.Background(), accountNumber)
			Expect(err).To(BeNil())

			number, err := wallet.AccountNumber(internalAccountName)
			By("returning a nil error")
			Expect(err).To(BeNil())
			By("returning the expected account number")
			Expect(number).To(BeEquivalentTo(accountNumber))
		})
	})

	Describe("HDPathForAccount", func() {
		It("should return the HD path for account", func() {
			var accountNumber int32 = 0
			path, err := wallet.HDPathForAccount(accountNumber)
			By("returning a nil error")
			Expect(err).To(BeNil())
			expectedPath := LegacyTestnetHDPath + strconv.Itoa(int(accountNumber))
			By("returning the expected path")
			Expect(path).To(BeEquivalentTo(expectedPath))
		})
	})
})

func randomPassword() string {
	var random = rand.New(rand.NewSource(time.Now().UnixNano()))
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = charset[random.Intn(len(charset))]
	}
	return string(b)
}
