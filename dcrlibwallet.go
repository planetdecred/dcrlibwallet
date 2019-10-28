package dcrlibwallet

import (
	"context"
	"path/filepath"

	"github.com/decred/dcrwallet/errors/v2"
	wallet "github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	l "github.com/raedahgroup/dcrlibwallet/internal/loader"
	"github.com/raedahgroup/dcrlibwallet/internal/netparams"
	"github.com/raedahgroup/dcrlibwallet/txindex"
	"github.com/raedahgroup/dcrlibwallet/utils"
)

const (
	SpendingPassphraseTypePin  int32 = 0
	SpendingPassphraseTypePass int32 = 1
)

type WalletProperties struct {
	WalletID               int    `storm:"id,increment"`
	WalletName             string `storm:"unique"`
	WalletDataDir          string
	WalletSeed             string
	DefaultAccount         int32
	SpendingPassphraseType int32
	DiscoveredAccounts     bool
}

type LibWallet struct {
	WalletProperties `storm:"inline"`

	activeNet    *netparams.Params
	walletLoader *l.Loader
	wallet       *wallet.Wallet
	txDB         *txindex.DB

	synced     bool
	syncing    bool
	waiting    bool
	rescanning bool

	shuttingDown chan bool
	cancelFuncs  []context.CancelFunc
}

func NewLibWallet(walletDataDir, walletDbDriver string, netType string) (*LibWallet, error) {

	activeNet := utils.NetParams(netType)
	if activeNet == nil {
		return nil, errors.E("unsupported network type: %s", netType)
	}

	lw := &LibWallet{
		activeNet: activeNet,
	}

	// open database for indexing transactions for faster loading
	txDBPath := filepath.Join(walletDataDir, txindex.DbName)
	var err error
	lw.txDB, err = txindex.Initialize(txDBPath, &Transaction{})
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// init walletLoader
	defaultFees := txrules.DefaultRelayFeePerKb.ToCoin()

	stakeOptions := &l.StakeOptions{
		VotingEnabled: false,
		AddressReuse:  false,
		VotingAddress: nil,
		TicketFee:     defaultFees,
	}

	lw.walletLoader = l.NewLoader(activeNet.Params, walletDataDir, stakeOptions, 20, false,
		defaultFees, wallet.DefaultAccountGapLimit, false)
	if walletDbDriver != "" {
		lw.walletLoader.SetDatabaseDriver(walletDbDriver)
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
