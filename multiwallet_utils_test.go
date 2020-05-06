package dcrlibwallet

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("MultiwalletUtils", func() {
	Describe("Wallet Seed Encryption", func() {
		Context("encryptWalletSeed and decryptWalletSeed", func() {
			It("encrypts and decrypts the wallet seed properly", func() {
				pass := []byte("pass")     // randomize
				newPass := []byte("passs") // randomize
				seed, err := GenerateSeed()
				Expect(err).To(BeNil())

				By("Encrypting the seed with the password")
				encrypted, err := encryptWalletSeed(pass, seed)
				Expect(err).To(BeNil())

				By("Failing decryption of the encrypted seed using the wrong password")
				_, err = decryptWalletSeed(newPass, encrypted)
				Expect(err).ToNot(BeNil())

				By("Decrypting the encrypted seed using the correct password")
				decrypted, err := decryptWalletSeed(pass, encrypted)
				Expect(err).To(BeNil())

				By("Comparing the decrypted and original seeds")
				Expect(seed).To(Equal(decrypted))
			})
		})
	})
})
