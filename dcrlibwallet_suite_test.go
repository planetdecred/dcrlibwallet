package dcrlibwallet_test

import (
	"os"
	"testing"

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

	RegisterFailHandler(Fail)
	RunSpecs(t, "Dcrlibwallet Suite")
}
