package dcrlibwallet_test

import (
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/raedahgroup/dcrlibwallet"
)

var _ = BeforeSuite(func() {
	SetLogLevels("ERROR")
})

const rootDir = ".dcrlibwallet_tests_root"

var _ = Describe("MultiWallet", func() {
	Describe("NewMultiWallet(rootDir, dbDriver, netType)", func() {
		AfterEach(func() {
			err := os.RemoveAll(rootDir)
			Expect(err).To(BeNil())
		})
		Context("when all arguments are valid", func() {
			It("should return succesfully", func() {
				multi, err := NewMultiWallet(rootDir, "", "testnet3")

				By("returning a nil error")
				Expect(err).To(BeNil())

				By("returning a non nil wallet")
				Expect(multi).ToNot(BeNil())

				multi.Shutdown()
			})
		})
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
		PContext(`when dbDriver is not "", "bdb" or "badgerdb"`, func() {
			It(`should fail when dbDriver == "nothing"`, func() {
				multi, err := NewMultiWallet(rootDir, "nothing", "testnet3")

				By("returning a nil wallet")
				Expect(multi == nil).To(Equal(true)) // panics otherwise

				By("returning an InvalidDriver error")
				Expect(err).To(Equal(ErrInvalidDriver))
			})
		})
	})
})
