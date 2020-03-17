package dcrlibwallet_test

import (
	"math/rand"
	"os"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/raedahgroup/dcrlibwallet"
)

const rootDir = ".dcrlibwallet_tests_root"

var _ = Describe("Account", func() {
	var (
		wallet *Wallet
		password string
	)

	BeforeEach(func() {
		multi, err := NewMultiWallet(rootDir, "", "testnet3")
		Expect(err).To(BeNil())
		Expect(multi).ToNot(BeNil())

		password = randomPassword()
		wallet, err = multi.CreateNewWallet(password, 0)
		Expect(err).To(BeNil())
		Expect(wallet).ToNot(BeNil())
	})

	AfterEach(func() {
		if wallet != nil {
			wallet.Shutdown()
		}
		err := os.RemoveAll(rootDir)
		Expect(err).To(BeNil())
	})

	Describe("GetAccounts", func() {
		Context("when called", func() {
			It("should get accounts", func() {
				res, err := wallet.GetAccounts(0)
				By("returning a nil error")
				Expect(err).To(BeNil())
				By("returning nonempty accounts string")
				Expect(res).ToNot(BeEquivalentTo(""))
			})
		})
	})

	Describe("GetAccount", func() {
		Context("when called with the right parameters", func() {
			It("should get account", func() {
				var accountNumber int32 = 0
				account, err := wallet.GetAccount(accountNumber, 0)
				By("returning a nil error")
				Expect(err).To(BeNil())
				By("returning a non nil account")
				Expect(account).ToNot(BeNil())
				By("returning an account with the supplied account number")
				Expect(account.Number).To(BeEquivalentTo(accountNumber))
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
		Context("when called", func() {
			It("should get the account balance", func() {
				balance, err := wallet.GetAccountBalance(0, 0)
				By("returning a nil error")
				Expect(err).To(BeNil())
				By("returning the expected balance")
				Expect(balance).ToNot(BeNil())
				Expect(balance.Total).To(BeEquivalentTo(0))
			})
		})
	})

	Describe("SpendableForAccount", func() {
		Context("when called", func() {
			It("should return the spendable balance", func() {
				balance, err := wallet.SpendableForAccount(0, 0)
				By("returning a nil error")
				Expect(err).To(BeNil())
				By("returning the expected balance")
				Expect(balance).To(BeEquivalentTo(0))
			})
		})
	})

	Describe("NextAccount", func() {
		Context("when called", func() {
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
		Context("when called", func() {
			It("should return the account name", func() {
				name := wallet.AccountName(0)
				By("returning the expected account name")
				Expect(name).To(BeEquivalentTo("default"))
			})
		})

		Context("when called with a non-existing account number", func() {
			It("should fail", func() {
				name := wallet.AccountName(1220)
				By("returning 'Account not found'")
				Expect(name).To(BeEquivalentTo("Account not found"))
			})
		})
	})

	Describe("AccountNameRaw", func() {
		Context("when called with a valid account number", func() {
			It("should return the account name", func() {
				name, err := wallet.AccountNameRaw(0)
				By("returning a nil error")
				Expect(err).To(BeNil())
				By("returning the expected account name")
				Expect(name).To(BeEquivalentTo("default"))
			})
		})
		Context("when called with a non-existing account number", func() {
			It("should fail", func() {
				_, err := wallet.AccountNameRaw(10290)
				By("returning a non nil error")
				Expect(err).ToNot(BeNil())
			})
		})
	})

	Describe("AccountNumber", func() {
		Context("when called", func() {
			It("should return the account number", func() {
				number, err := wallet.AccountNumber("default")
				By("returning a nil error")
				Expect(err).To(BeNil())
				By("returning the expected account number")
				Expect(number).To(BeEquivalentTo(uint32(0)))
			})
		})
	})

	Describe("HDPathForAccount", func() {
		Context("when called", func() {
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
