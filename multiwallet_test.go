package dcrlibwallet_test

import (
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/raedahgroup/dcrlibwallet"
)

var _ = Describe("MultiWallet", func() {
	Context("NewMultiWallet", func() {
		It("returns a InvalidNetwork error with a wrong netType", func() {
			dir := ".dcrlibwallet"
			multi, err := NewMultiWallet(dir, "", "")
			Expect(multi).To(BeNil())
			Expect(err).To(Equal(ErrInvalidNetwork))
			_, err = os.Stat(dir)
			Expect(os.IsNotExist(err)).To(Equal(true))
		})
	})
})
