package dcrlibwallet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	w "decred.org/dcrwallet/v2/wallet"
	"decred.org/dcrwallet/v2/walletseed"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/planetdecred/dcrlibwallet/internal/loader"
	"github.com/planetdecred/dcrlibwallet/internal/vsp"
	"github.com/planetdecred/dcrlibwallet/walletdata"
)

type Wallet struct {
	ID                    int       `storm:"id,increment"`
	Name                  string    `storm:"unique"`
	CreatedAt             time.Time `storm:"index"`
	DbDriver              string
	EncryptedSeed         []byte
	IsRestored            bool
	HasDiscoveredAccounts bool
	PrivatePassphraseType int32

	chainParams  *chaincfg.Params
	dataDir      string
	loader       *loader.Loader
	walletDataDB *walletdata.DB

	synced            bool
	syncing           bool
	waitingForHeaders bool

	shuttingDown       chan bool
	cancelFuncs        []context.CancelFunc
	cancelAccountMixer context.CancelFunc

	cancelAutoTicketBuyerMu sync.Mutex
	cancelAutoTicketBuyer   context.CancelFunc

	vspClientsMu sync.Mutex
	vspClients   map[string]*vsp.Client

	// setUserConfigValue saves the provided key-value pair to a config database.
	// This function is ideally assigned when the `wallet.prepare` method is
	// called from a MultiWallet instance.
	setUserConfigValue configSaveFn

	// readUserConfigValue returns the previously saved value for the provided
	// key from a config database. Returns nil if the key wasn't previously set.
	// This function is ideally assigned when the `wallet.prepare` method is
	// called from a MultiWallet instance.
	readUserConfigValue configReadFn
}

// prepare gets a wallet ready for use by opening the transactions index database
// and initializing the wallet loader which can be used subsequently to create,
// load and unload the wallet.
func (wallet *Wallet) prepare(rootDir string, chainParams *chaincfg.Params,
	setUserConfigValueFn configSaveFn, readUserConfigValueFn configReadFn) (err error) {

	wallet.chainParams = chainParams
	wallet.dataDir = filepath.Join(rootDir, strconv.Itoa(wallet.ID))
	wallet.vspClients = make(map[string]*vsp.Client)
	wallet.setUserConfigValue = setUserConfigValueFn
	wallet.readUserConfigValue = readUserConfigValueFn

	// open database for indexing transactions for faster loading
	walletDataDBPath := filepath.Join(wallet.dataDir, walletdata.DbName)
	oldTxDBPath := filepath.Join(wallet.dataDir, walletdata.OldDbName)
	if exists, _ := fileExists(oldTxDBPath); exists {
		moveFile(oldTxDBPath, walletDataDBPath)
	}
	wallet.walletDataDB, err = walletdata.Initialize(walletDataDBPath, chainParams, &Transaction{})
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// init loader
	wallet.loader = initWalletLoader(wallet.chainParams, wallet.dataDir, wallet.DbDriver)

	// init cancelFuncs slice to hold cancel functions for long running
	// operations and start go routine to listen for shutdown signal
	wallet.cancelFuncs = make([]context.CancelFunc, 0)
	wallet.shuttingDown = make(chan bool)
	go func() {
		<-wallet.shuttingDown
		for _, cancel := range wallet.cancelFuncs {
			cancel()
		}
	}()

	return nil
}

func (wallet *Wallet) Shutdown() {
	// Trigger shuttingDown signal to cancel all contexts created with
	// `wallet.shutdownContext()` or `wallet.shutdownContextWithCancel()`.
	wallet.shuttingDown <- true

	if _, loaded := wallet.loader.LoadedWallet(); loaded {
		err := wallet.loader.UnloadWallet()
		if err != nil {
			log.Errorf("Failed to close wallet: %v", err)
		} else {
			log.Info("Closed wallet")
		}
	}

	if wallet.walletDataDB != nil {
		err := wallet.walletDataDB.Close()
		if err != nil {
			log.Errorf("tx db closed with error: %v", err)
		} else {
			log.Info("tx db closed successfully")
		}
	}
}

// WalletCreationTimeInMillis returns the wallet creation time for new
// wallets. Restored wallets would return an error.
func (wallet *Wallet) WalletCreationTimeInMillis() (int64, error) {
	if wallet.IsRestored {
		return 0, errors.New(ErrWalletIsRestored)
	}

	return wallet.CreatedAt.UnixNano() / int64(time.Millisecond), nil
}

func (wallet *Wallet) NetType() string {
	return wallet.chainParams.Name
}

func (wallet *Wallet) Internal() *w.Wallet {
	lw, _ := wallet.loader.LoadedWallet()
	return lw
}

func (wallet *Wallet) WalletExists() (bool, error) {
	return wallet.loader.WalletExists()
}

func (wallet *Wallet) createWallet(privatePassphrase, seedMnemonic string) error {
	log.Info("Creating Wallet")
	if len(seedMnemonic) == 0 {
		return errors.New(ErrEmptySeed)
	}

	pubPass := []byte(w.InsecurePubPassphrase)
	privPass := []byte(privatePassphrase)
	seed, err := walletseed.DecodeUserInput(seedMnemonic)
	if err != nil {
		log.Error(err)
		return err
	}

	_, err = wallet.loader.CreateNewWallet(wallet.shutdownContext(), pubPass, privPass, seed)
	if err != nil {
		log.Error(err)
		return err
	}

	log.Info("Created Wallet")
	return nil
}

func (wallet *Wallet) createWatchingOnlyWallet(extendedPublicKey string) error {
	pubPass := []byte(w.InsecurePubPassphrase)

	_, err := wallet.loader.CreateWatchingOnlyWallet(wallet.shutdownContext(), extendedPublicKey, pubPass)
	if err != nil {
		log.Error(err)
		return err
	}

	log.Info("Created Watching Only Wallet")
	return nil
}

func (wallet *Wallet) IsWatchingOnlyWallet() bool {
	if w, ok := wallet.loader.LoadedWallet(); ok {
		return w.WatchingOnly()
	}

	return false
}

func (wallet *Wallet) openWallet() error {
	pubPass := []byte(w.InsecurePubPassphrase)

	_, err := wallet.loader.OpenExistingWallet(wallet.shutdownContext(), pubPass)
	if err != nil {
		log.Error(err)
		return translateError(err)
	}

	return nil
}

func (wallet *Wallet) WalletOpened() bool {
	return wallet.Internal() != nil
}

func (wallet *Wallet) UnlockWallet(privPass []byte) error {
	loadedWallet, ok := wallet.loader.LoadedWallet()
	if !ok {
		return fmt.Errorf("wallet has not been loaded")
	}

	ctx, _ := wallet.shutdownContextWithCancel()
	err := loadedWallet.Unlock(ctx, privPass, nil)
	if err != nil {
		return translateError(err)
	}

	return nil
}

func (wallet *Wallet) LockWallet() {
	if wallet.IsAccountMixerActive() {
		log.Error("LockWallet ignored due to active account mixer")
		return
	}

	if !wallet.Internal().Locked() {
		wallet.Internal().Lock()
	}
}

func (wallet *Wallet) IsLocked() bool {
	return wallet.Internal().Locked()
}

func (wallet *Wallet) changePrivatePassphrase(oldPass []byte, newPass []byte) error {
	defer func() {
		for i := range oldPass {
			oldPass[i] = 0
		}

		for i := range newPass {
			newPass[i] = 0
		}
	}()

	err := wallet.Internal().ChangePrivatePassphrase(wallet.shutdownContext(), oldPass, newPass)
	if err != nil {
		return translateError(err)
	}
	return nil
}

func (wallet *Wallet) deleteWallet(privatePassphrase []byte) error {
	defer func() {
		for i := range privatePassphrase {
			privatePassphrase[i] = 0
		}
	}()

	if _, loaded := wallet.loader.LoadedWallet(); !loaded {
		return errors.New(ErrWalletNotLoaded)
	}

	if !wallet.IsWatchingOnlyWallet() {
		err := wallet.Internal().Unlock(wallet.shutdownContext(), privatePassphrase, nil)
		if err != nil {
			return translateError(err)
		}
		wallet.Internal().Lock()
	}

	wallet.Shutdown()

	log.Info("Deleting Wallet")
	return os.RemoveAll(wallet.dataDir)
}

// DecryptSeed decrypts wallet.EncryptedSeed using privatePassphrase
func (wallet *Wallet) DecryptSeed(privatePassphrase []byte) (string, error) {
	if wallet.EncryptedSeed == nil {
		return "", errors.New(ErrInvalid)
	}

	return decryptWalletSeed(privatePassphrase, wallet.EncryptedSeed)
}

// GetVoteChoices handles a getvotechoices request by returning configured vote
// preferences for each agenda of the latest supported stake version.
func (wallet *Wallet) GetVoteChoices(ctx context.Context, hash string) (*types.GetVoteChoicesResult, error) {
	wal := wallet.Internal()
	if wal == nil {
		return nil, fmt.Errorf("request requires a wallet but wallet has not loaded yet")
	}

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return nil, fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

	version, agendas := w.CurrentAgendas(wal.ChainParams())
	resp := &types.GetVoteChoicesResult{
		Version: version,
		Choices: make([]types.VoteChoice, len(agendas)),
	}

	choices, _, err := wal.AgendaChoices(ctx, ticketHash)
	if err != nil {
		return nil, err
	}

	for i := range choices {
		resp.Choices[i] = types.VoteChoice{
			AgendaID:          choices[i].AgendaID,
			AgendaDescription: agendas[i].Vote.Description,
			ChoiceID:          choices[i].ChoiceID,
			ChoiceDescription: "", // Set below
		}
		for j := range agendas[i].Vote.Choices {
			if choices[i].ChoiceID == agendas[i].Vote.Choices[j].Id {
				resp.Choices[i].ChoiceDescription = agendas[i].Vote.Choices[j].Description
				break
			}
		}
	}

	return resp, nil
}

// SetVoteChoice handles a setvotechoice request by modifying the preferred
// choice for a voting agenda.
//
// If a VSP host is configured in the application settings, the voting
// preferences will also be set with the VSP.
func (wallet *Wallet) SetVoteChoice(ctx context.Context, vspHost, agendaID, choiceID, hash string) error {
	wal := wallet.Internal()
	if wal == nil {
		return fmt.Errorf("request requires a wallet but wallet has not loaded yet")
	}

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

	choice := w.AgendaChoice{
		AgendaID: agendaID,
		ChoiceID: choiceID,
	}

	_, err := wal.SetAgendaChoices(ctx, ticketHash, choice)
	if err != nil {
		return err
	}

	if vspHost == "" {
		return nil
	}
	// vspClient, err := loader.LookupVSP(vspHost)
	// if err != nil {
	// 	return nil, err
	// }
	var mw MultiWallet
	vsp, err := mw.NewVSPClient(vspHost, 1, 0)
	// vspClient, err := LookupVSP(vspHost)
	if err != nil {
		return err
	}
	if ticketHash != nil {
		err = vsp.SetVoteChoice(ctx, ticketHash, choice)
		return err
	}
	var firstErr error
	vsp.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		// Never return errors here, so all tickets are tried.
		// The first error will be returned to the user.
		err := vsp.SetVoteChoice(ctx, hash, choice)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return nil
	})
	return firstErr
}
