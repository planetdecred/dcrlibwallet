package dcrlibwallet_test

import (
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/raedahgroup/dcrlibwallet"
)

const rootDir = ".dcrlibwallet_tests_root"

var _ = BeforeSuite(func() {
	_, err := os.Stat(rootDir)
	Expect(os.IsNotExist(err)).To(Equal(true))
})

// TODO: Add cleanup to delete rootDir if it exists

var _ = Describe("MultiWallet", func() {
	Describe("NewMultiWallet(rootDir, dbDriver, netType)", func() {
		Context("when netType is not a valid network", func() {
			It("shoud fail", func() {
				multi, err := NewMultiWallet(rootDir, "", "")
				By("returning a nil wallet")
				Expect(multi).To(BeNil())

				By("returning an ErrInvalidNetwork")
				Expect(err).To(Equal(ErrInvalidNetwork))

				By("not creating the directory")
				_, err = os.Stat(rootDir)
				Expect(os.IsNotExist(err)).To(Equal(true))
			})
		})
	})
})
