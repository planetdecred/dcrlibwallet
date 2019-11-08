package dcrlibwallet

import (
	"encoding/hex"
	"fmt"

	"github.com/decred/dcrd/dcrutil/v2"
	wallet "github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/udb"
)

// AddressInfo holds information about an address
// If the address belongs to the querying wallet, IsMine will be true and the AccountNumber and AccountName values will be populated
type AddressInfo struct {
	Address       string
	IsMine        bool
	AccountNumber uint32
	AccountName   string
}

func (lw *LibWallet) IsAddressValid(address string) bool {
	_, err := dcrutil.DecodeAddress(address, lw.activeNet.Params)
	return err == nil
}

func (lw *LibWallet) HaveAddress(address string) bool {
	addr, err := dcrutil.DecodeAddress(address, lw.activeNet.Params)
	if err != nil {
		return false
	}

	ctx := lw.shutdownContext()
	have, err := lw.wallet.HaveAddress(ctx, addr)
	if err != nil {
		return false
	}

	return have
}

func (lw *LibWallet) AccountOfAddress(address string) string {
	addr, err := dcrutil.DecodeAddress(address, lw.activeNet.Params)
	if err != nil {
		return err.Error()
	}

	ctx := lw.shutdownContext()
	info, _ := lw.wallet.AddressInfo(ctx, addr)
	return lw.AccountName(int32(info.Account()))
}

func (lw *LibWallet) AddressInfo(address string) (*AddressInfo, error) {
	addr, err := dcrutil.DecodeAddress(address, lw.activeNet.Params)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	addressInfo := &AddressInfo{
		Address: address,
	}

	ctx := lw.shutdownContext()
	info, _ := lw.wallet.AddressInfo(ctx, addr)
	if info != nil {
		addressInfo.IsMine = true
		addressInfo.AccountNumber = info.Account()
		addressInfo.AccountName = lw.AccountName(int32(info.Account()))
	}

	return addressInfo, nil
}

func (lw *LibWallet) CurrentAddress(account int32) (string, error) {
	addr, err := lw.wallet.CurrentAddress(uint32(account))
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.Address(), nil
}

func (lw *LibWallet) NextAddress(account int32) (string, error) {
	ctx := lw.shutdownContext()

	addr, err := lw.wallet.NewExternalAddress(ctx, uint32(account), wallet.WithGapPolicyWrap())
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.Address(), nil
}

func (lw *LibWallet) AddressPubKey(address string) (string, error) {
	addr, err := dcrutil.DecodeAddress(address, lw.activeNet.Params)
	if err != nil {
		return "", err
	}

	ctx := lw.shutdownContext()
	ainfo, err := lw.wallet.AddressInfo(ctx, addr)
	if err != nil {
		return "", err
	}
	switch ma := ainfo.(type) {
	case udb.ManagedPubKeyAddress:
		pubKey := ma.ExportPubKey()
		pubKeyBytes, err := hex.DecodeString(pubKey)
		if err != nil {
			return "", err
		}
		pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(pubKeyBytes, lw.activeNet.Params)
		if err != nil {
			return "", err
		}
		return pubKeyAddr.String(), nil

	default:
		return "", fmt.Errorf("address is not a managed pub key address")
	}
}
