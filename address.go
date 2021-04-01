package dcrlibwallet

import (
	"fmt"

	"decred.org/dcrwallet/errors"
	w "decred.org/dcrwallet/wallet"
	"github.com/decred/dcrd/dcrutil/v3"
)

// AddressInfo holds information about an address
// If the address belongs to the querying wallet, IsMine will be true and the AccountNumber and AccountName values will be populated
type AddressInfo struct {
	Address       string
	IsMine        bool
	AccountNumber uint32
	AccountName   string
}

func (mw *MultiWallet) IsAddressValid(address string) bool {
	_, err := dcrutil.DecodeAddress(address, mw.chainParams)
	return err == nil
}

func (wallet *Wallet) HaveAddress(address string) bool {
	addr, err := dcrutil.DecodeAddress(address, wallet.chainParams)
	if err != nil {
		return false
	}

	have, err := wallet.internal.HaveAddress(wallet.shutdownContext(), addr)
	if err != nil {
		return false
	}

	return have
}

func (wallet *Wallet) AccountOfAddress(address string) (string, error) {
	addr, err := dcrutil.DecodeAddress(address, wallet.chainParams)
	if err != nil {
		return "", translateError(err)
	}

	a, err := wallet.internal.KnownAddress(wallet.shutdownContext(), addr)
	if err != nil {
		return "", translateError(err)
	}

	return a.AccountName(), nil
}

func (wallet *Wallet) AddressInfo(address string) (*AddressInfo, error) {
	addr, err := dcrutil.DecodeAddress(address, wallet.chainParams)
	if err != nil {
		return nil, err
	}

	addressInfo := &AddressInfo{
		Address: address,
	}

	known, _ := wallet.internal.KnownAddress(wallet.shutdownContext(), addr)
	if known != nil {
		addressInfo.IsMine = true
		addressInfo.AccountName = known.AccountName()

		accountNumber, err := wallet.AccountNumber(known.AccountName())
		if err != nil {
			return nil, err
		}
		addressInfo.AccountNumber = accountNumber
	}

	return addressInfo, nil
}

func (wallet *Wallet) CurrentAddress(account int32) (string, error) {
	if wallet.IsRestored && !wallet.HasDiscoveredAccounts {
		return "", errors.E(ErrAddressDiscoveryNotDone)
	}

	addr, err := wallet.internal.CurrentAddress(uint32(account))
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.Address(), nil
}

func (wallet *Wallet) NextAddress(account int32) (string, error) {
	if wallet.IsRestored && !wallet.HasDiscoveredAccounts {
		return "", errors.E(ErrAddressDiscoveryNotDone)
	}

	addr, err := wallet.internal.NewExternalAddress(wallet.shutdownContext(), uint32(account), w.WithGapPolicyWrap())
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.Address(), nil
}

func (wallet *Wallet) AddressPubKey(address string) (string, error) {
	addr, err := dcrutil.DecodeAddress(address, wallet.chainParams)
	if err != nil {
		return "", err
	}

	known, err := wallet.internal.KnownAddress(wallet.shutdownContext(), addr)
	if err != nil {
		return "", err
	}

	switch known := known.(type) {
	case w.PubKeyHashAddress:

		pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(known.PubKey(), wallet.chainParams)
		if err != nil {
			return "", err
		}
		return pubKeyAddr.String(), nil
	default:
		return "", fmt.Errorf("address is not a managed pub key address")
	}
}
