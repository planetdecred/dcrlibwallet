package dcrlibwallet_test

import (
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/raedahgroup/dcrlibwallet"
)

const rootDir = ".dcrlibwallet_tests_root"

var _ = Describe("MultiWallet", func() {
	Describe("NewMultiWallet(rootDir, dbDriver, netType)", func() {
		Context("when netType is not a valid network", func() {
			It("should fail", func() {
				_, err := os.Stat(rootDir)
				didNotExist := os.IsNotExist(err)
				multi, err := NewMultiWallet(rootDir, "", "") // TODO: Test with other strings
				By("returning a nil wallet")
				Expect(multi).To(BeNil())

				By("returning an ErrInvalidNetwork")
				Expect(err).To(Equal(ErrInvalidNetwork))

				By("not creating the directory if it doesn't exist")
				_, err = os.Stat(rootDir)
				Expect(os.IsNotExist(err)).To(Equal(didNotExist))
			})
		})
	})
})
