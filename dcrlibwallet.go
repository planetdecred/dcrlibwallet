package dcrlibwallet

import (
	"context"
	"os"
	"path/filepath"

	"github.com/asdine/storm"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/netparams"
	wallet "github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	"github.com/raedahgroup/dcrlibwallet/txindex"
	"github.com/raedahgroup/dcrlibwallet/utils"
	bolt "go.etcd.io/bbolt"
)

errors.Separator = ":: "

const logFileName = "dcrlibwallet.log"

type LibWallet struct {
	WalletID      int `storm:"id,increment"`
	WalletName    string
	WalletDataDir string
	WalletSeed    string

	activeNet    *netparams.Params
	walletLoader *WalletLoader
	wallet       *wallet.Wallet
	txDB         *txindex.DB
	configDB     *storm.DB

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
		activeNet:     activeNet,
	} 

	// open database for indexing transactions for faster loading
	txDBPath := filepath.Join(lw.WalletDataDir, txindex.DbName)
	lw.txDB, err = txindex.Initialize(txDBPath, &Transaction{})
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// init walletLoader
	defaultFees := txrules.DefaultRelayFeePerKb.ToCoin()

	stakeOptions := &StakeOptions{
		VotingEnabled: false,
		AddressReuse:  false,
		VotingAddress: nil,
		TicketFee:     defaultFees,
	}

	lw.walletLoader = NewLoader(activeNet.Params, lw.WalletDataDir, stakeOptions, 20, false,
		defaultFees, wallet.DefaultAccountGapLimit)
	if walletDbDriver != "" {
		lw.walletLoader.SetDatabaseDriver(walletDbDriver)
	}

	lw.syncData = &syncData{
		syncCanceled:          make(chan bool),
		syncProgressListeners: make(map[string]SyncProgressListener),
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
