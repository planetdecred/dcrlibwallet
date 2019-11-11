package dcrlibwallet

import (
	"context"
	"path/filepath"

	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	"github.com/raedahgroup/dcrlibwallet/internal/loader"
	"github.com/raedahgroup/dcrlibwallet/internal/netparams"
	"github.com/raedahgroup/dcrlibwallet/txindex"
	"github.com/raedahgroup/dcrlibwallet/utils"
)

type Wallet struct {
	*wallet.Wallet `json:"-"`
	netType        string
	walletDbDriver string

	ID                     int    `storm:"id,increment"`
	Name                   string `storm:"unique"`
	DataDir                string
	Seed                   string
	SpendingPassphraseType int32
	DiscoveredAccounts     bool
}

type LibWallet struct {
	wallet *Wallet

	activeNet    *netparams.Params
	walletLoader *loader.Loader
	txDB         *txindex.DB

	synced     bool
	syncing    bool
	waiting    bool
	rescanning bool

	shuttingDown chan bool
	cancelFuncs  []context.CancelFunc
}

func NewLibWallet(w *Wallet) (*LibWallet, error) {

	activeNet := utils.NetParams(w.netType)
	if activeNet == nil {
		return nil, errors.E("unsupported network type: %s", w.netType)
	}

	lw := &LibWallet{
		wallet:    w,
		activeNet: activeNet,
	}

	// open database for indexing transactions for faster loading
	txDBPath := filepath.Join(w.DataDir, txindex.DbName)
	var err error
	lw.txDB, err = txindex.Initialize(txDBPath, &Transaction{})
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// init walletLoader
	defaultFees := txrules.DefaultRelayFeePerKb.ToCoin()

	stakeOptions := &loader.StakeOptions{
		VotingEnabled: false,
		AddressReuse:  false,
		VotingAddress: nil,
		TicketFee:     defaultFees,
	}

	lw.walletLoader = loader.NewLoader(activeNet.Params, w.DataDir, stakeOptions, 20, false,
		defaultFees, wallet.DefaultAccountGapLimit, false)
	if w.walletDbDriver != "" {
		lw.walletLoader.SetDatabaseDriver(w.walletDbDriver)
	}

	// todo add interrupt listener
	lw.listenForShutdown()

	return lw, nil
}

func (lw *LibWallet) Shutdown() {

	// Trigger shuttingDown signal to cancel all contexts created with `contextWithShutdownCancel`.
	lw.shuttingDown <- true

	if _, loaded := lw.walletLoader.LoadedWallet(); loaded {
		err := lw.walletLoader.UnloadWallet()
		if err != nil {
			log.Errorf("Failed to close wallet: %v", err)
		} else {
			log.Info("Closed wallet")
		}
	}

	if lw.txDB != nil {
		err := lw.txDB.Close()
		if err != nil {
			log.Errorf("tx db closed with error: %v", err)
		} else {
			log.Info("tx db closed successfully")
		}
	}
}
