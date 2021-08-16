package dcrlibwallet

import (
	"decred.org/dcrwallet/errors"
	w "decred.org/dcrwallet/wallet"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v3"
)

func (wallet *Wallet) SignMessage(passphrase []byte, address string, message string) ([]byte, error) {
	err := wallet.UnlockWallet(passphrase)
	if err != nil {
		return nil, translateError(err)
	}
	defer wallet.LockWallet()

	return wallet.signMessage(address, message)
}

func (wallet *Wallet) signMessage(address string, message string) ([]byte, error) {
	addr, err := dcrutil.DecodeAddress(address, wallet.chainParams)
	if err != nil {
		return nil, translateError(err)
	}

	var sig []byte
	switch a := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
	case *dcrutil.AddressPubKeyHash:
		if a.DSA() != dcrec.STEcdsaSecp256k1 {
			return nil, errors.New(ErrInvalidAddress)
		}
	default:
		return nil, errors.New(ErrInvalidAddress)
	}

	sig, err = wallet.internal.SignMessage(wallet.shutdownContext(), message, addr)
	if err != nil {
		return nil, translateError(err)
	}

	return sig, nil
}

func (mw *MultiWallet) VerifyMessage(address string, message string, signatureBase64 string) (bool, error) {
	var valid bool

	addr, err := dcrutil.DecodeAddress(address, mw.chainParams)
	if err != nil {
		return false, translateError(err)
	}

	signature, err := DecodeBase64(signatureBase64)
	if err != nil {
		return false, err
	}

	// Addresses must have an associated secp256k1 private key and therefore
	// must be P2PK or P2PKH (P2SH is not allowed).
	switch a := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
	case *dcrutil.AddressPubKeyHash:
		if a.DSA() != dcrec.STEcdsaSecp256k1 {
			return false, errors.New(ErrInvalidAddress)
		}
	default:
		return false, errors.New(ErrInvalidAddress)
	}

	valid, err = w.VerifyMessage(message, addr, signature, mw.chainParams)
	if err != nil {
		return false, translateError(err)
	}

	return valid, nil
}
