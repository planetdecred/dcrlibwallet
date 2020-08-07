package dcrlibwallet

import (
	"errors"

	"decred.org/dcrwallet/ticketbuyer"
	w "decred.org/dcrwallet/wallet"
	"github.com/decred/dcrd/dcrutil/v3"
)

const smalletSplitPoint = 000.00262144

func (mw *MultiWallet) SetAccountMixerNotification(accountMixerNotificationListener AccountMixerNotificationListener) {
	mw.accountMixerNotificationListener = accountMixerNotificationListener
}

func (wallet *Wallet) SetAccountMixerConfig(mixedAccount, changeAccount int32) error {

	accountMixerConfigSet := wallet.ReadBoolConfigValueForKey(AccountMixerConfigSet, false)
	if accountMixerConfigSet {
		return errors.New(ErrInvalid)
	}

	_, err := wallet.GetAccount(mixedAccount)
	if err != nil {
		return err
	}

	_, err = wallet.GetAccount(changeAccount)
	if err != nil {
		return err
	}

	wallet.SetInt32ConfigValueForKey(AccountMixerMixedAccount, mixedAccount)
	wallet.SetInt32ConfigValueForKey(AccountMixerChangeAccount, changeAccount)
	wallet.SetBoolConfigValueForKey(AccountMixerConfigSet, true)

	return nil
}

// StartAccountMixer starts the automatic account mixer
func (mw *MultiWallet) StartAccountMixer(walletID int, walletPassphrase string) error {

	if !mw.IsConnectedToDecredNetwork() {
		return errors.New(ErrNotConnected)
	}

	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	tb := ticketbuyer.New(wallet.internal)

	mixedAccount := wallet.ReadInt32ConfigValueForKey(AccountMixerMixedAccount, -1)
	changeAccount := wallet.ReadInt32ConfigValueForKey(AccountMixerChangeAccount, -1)

	hasMixableOutput, err := wallet.accountHasMixableOutput(changeAccount)
	if err != nil {
		return translateError(err)
	} else if !hasMixableOutput {
		return errors.New(ErrNoMixableOutput)
	}

	tb.AccessConfig(func(c *ticketbuyer.Config) {
		c.MixedAccountBranch = 0
		c.MixedAccount = uint32(mixedAccount)
		c.ChangeAccount = uint32(changeAccount)
		c.CSPPServer = "cspp.decred.org:15760"
		c.BuyTickets = false
		c.MixChange = true
	})

	err = wallet.UnlockWallet([]byte(walletPassphrase))
	if err != nil {
		return translateError(err)
	}

	go func() {
		log.Info("Running account mixer")
		if mw.accountMixerNotificationListener != nil {
			mw.accountMixerNotificationListener.OnAccountMixerStarted(walletID)
		}

		ctx, cancel := mw.contextWithShutdownCancel()
		wallet.cancelAccountMixer = cancel
		err = tb.Run(ctx, []byte(walletPassphrase))
		if err != nil {
			log.Errorf("AccountMixer instance errored: %v", err)
		}

		wallet.cancelAccountMixer = nil
		if mw.accountMixerNotificationListener != nil {
			mw.accountMixerNotificationListener.OnAccountMixerEnded(walletID)
		}
	}()

	return nil
}

// StopAccountMixer stops the active account mixer
func (mw *MultiWallet) StopAccountMixer(walletID int) error {

	wallet := mw.WalletWithID(walletID)
	if wallet == nil {
		return errors.New(ErrNotExist)
	}

	if wallet.cancelAccountMixer == nil {
		return errors.New(ErrInvalid)
	}

	wallet.cancelAccountMixer()
	wallet.cancelAccountMixer = nil
	return nil
}

func (wallet *Wallet) accountHasMixableOutput(accountNumber int32) (bool, error) {

	policy := w.OutputSelectionPolicy{
		Account:               uint32(accountNumber),
		RequiredConfirmations: wallet.RequiredConfirmations(),
	}

	// fetch all utxos in account to extract details for the utxos selected by user
	// use targetAmount = 0 to fetch ALL utxos in account
	inputDetail, err := wallet.internal.SelectInputs(wallet.shutdownContext(), dcrutil.Amount(0), policy)
	if err != nil {
		return false, nil
	}

	hasMixableOutput := false
	for _, input := range inputDetail.Inputs {
		log.Info("Credit: ", dcrutil.Amount(input.ValueIn))
		if AmountCoin(input.ValueIn) > smalletSplitPoint {
			hasMixableOutput = true
			break
		}
	}

	if !hasMixableOutput {
		accoutnName, err := wallet.AccountName(accountNumber)
		if err != nil {
			return hasMixableOutput, nil
		}

		lockedOutpoints, err := wallet.internal.LockedOutpoints(wallet.shutdownContext(), accoutnName)
		if err != nil {
			return hasMixableOutput, nil
		}

		log.Infof("Checking %d locked outpoints", len(lockedOutpoints))
		hasMixableOutput = len(lockedOutpoints) > 0
	}

	return hasMixableOutput, nil
}

// IsAccountMixerActive returns true if account mixer is active
func (wallet *Wallet) IsAccountMixerActive() bool {
	return wallet.cancelAccountMixer != nil
}
