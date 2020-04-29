package dcrlibwallet_test

import (
	"math/rand"
	"os"
	"testing"
	"time"

	w "github.com/decred/dcrwallet/wallet/v3"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/raedahgroup/dcrlibwallet"
)

var (
	wallet         *Wallet
	internalWallet *w.Wallet
	password       string
)

func TestDcrlibwallet(t *testing.T) {

	BeforeSuite(func() {
		os.RemoveAll(rootDir)
		multi, err := NewMultiWallet(rootDir, "", "testnet3")
		Expect(err).To(BeNil())
		Expect(multi).ToNot(BeNil())

		password = randomPassword()
		wallet, err = multi.CreateNewWallet("main", password, 0)
		Expect(err).To(BeNil())
		Expect(wallet).ToNot(BeNil())
		wallet.SaveUserConfigValue(SpendUnconfirmedConfigKey, true)
		internalWallet = wallet.InternalWallet()
	})

	AfterSuite(func() {
		if wallet != nil {
			wallet.Shutdown()
		}
	})

	RegisterFailHandler(Fail)
	RunSpecs(t, "Dcrlibwallet Suite")
}

func randomPassword() string {
	var random = rand.New(rand.NewSource(time.Now().UnixNano()))
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = charset[random.Intn(len(charset))]
	}
	return string(b)
}
