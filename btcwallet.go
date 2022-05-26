package dcrlibwallet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"decred.org/dcrwallet/v2/errors"
	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/planetdecred/dcrlibwallet/btcwallet"
)

const walletTypeSPV = "SPV"

type btcWallet struct {
	db            *storm.DB
	wallets       map[int]*btcwallet.Wallet
	cancelCoreCtx context.CancelFunc
	btcDataDir    string
}

func (mw *MultiWallet) initBtcWallet() error {
	if mw.btcWallet != nil {
		return nil
	}

	btcDataDir := filepath.Join(mw.rootDir, "btc")

	err := os.MkdirAll(btcDataDir, os.ModePerm)
	if err != nil {
		return err
	}

	walletsDB, err := storm.Open(filepath.Join(btcDataDir, walletsDbName))
	if err != nil {
		return err
	}

	// init database for saving/reading wallet objects
	err = walletsDB.Init(&btcwallet.Wallet{})
	if err != nil {
		log.Errorf("Error initializing wallets database BTC: %s", err.Error())
		return err
	}

	mw.btcWallet = &btcWallet{
		btcDataDir: filepath.Join(mw.rootDir, "btc"),
		db:         walletsDB,
		wallets:    make(map[int]*btcwallet.Wallet),
	}

	// read saved wallets info from db and initialize wallets
	query := walletsDB.Select(q.True()).OrderBy("ID")
	var wallets []*btcwallet.Wallet
	err = query.Find(&wallets)
	if err != nil && err != storm.ErrNotFound {
		return err
	}

	// prepare the wallets loaded from db for use
	for _, wallet := range wallets {
		err = wallet.Prepare(mw.btcWallet.btcDataDir, mw.NetType(), log)
		if err == nil && !WalletExistsAt(wallet.DataDir()) {
			err = fmt.Errorf("missing wallet database file")
		}
		if err != nil {
			log.Warnf("Ignored wallet load error for wallet %d (%s)", wallet.ID, wallet.Name)
		} else {
			mw.btcWallet.wallets[wallet.ID] = wallet
		}
	}

	return nil
}

func (mw *MultiWallet) CreateNewBTCWallet(walletName, password string) (*btcwallet.Wallet, error) {
	seed := "witch collapse practice feed shame open despair"
	encryptedSeed := []byte(seed)

	wallet, err := btcwallet.NewSpvWallet(walletName, encryptedSeed, mw.NetType(), log)
	if err != nil {
		return nil, err
	}

	walletNameExists := func(walletName string) (bool, error) {
		if strings.HasPrefix(walletName, "wallet-") {
			return false, errors.E(ErrReservedWalletName)
		}

		err := mw.btcWallet.db.One("Name", walletName, &btcwallet.Wallet{})
		if err == nil {
			return true, nil
		} else if err != storm.ErrNotFound {
			return false, err
		}

		return false, nil
	}

	exists, err := walletNameExists(wallet.Name)
	if err != nil {
		return nil, err
	} else if exists {
		return nil, errors.New(ErrExist)
	}

	batchDbTransaction := func(dbOp func(node storm.Node) error) (err error) {
		dbTx, err := mw.btcWallet.db.Begin(true)
		if err != nil {
			return err
		}

		// Commit or rollback the transaction after f returns or panics.  Do not
		// recover from the panic to keep the original stack trace intact.
		panicked := true
		defer func() {
			if panicked || err != nil {
				dbTx.Rollback()
				return
			}

			err = dbTx.Commit()
		}()

		err = dbOp(dbTx)
		panicked = false
		return err
	}

	// Perform database save operations in batch transaction
	// for automatic rollback if error occurs at any point.
	err = batchDbTransaction(func(db storm.Node) error {
		// saving struct to update ID property with an auto-generated value
		err := db.Save(wallet)
		if err != nil {
			return err
		}
		walletDataDir := filepath.Join(mw.btcWallet.btcDataDir, strconv.Itoa(wallet.ID))

		dirExists, err := fileExists(walletDataDir)
		if err != nil {
			return err
		} else if dirExists {
			newDirName, err := backupFile(walletDataDir, 1)
			if err != nil {
				return err
			}

			log.Infof("Undocumented file at %s moved to %s", walletDataDir, newDirName)
		}

		os.MkdirAll(walletDataDir, os.ModePerm) // create wallet dir

		if wallet.Name == "" {
			wallet.Name = "wallet-" + strconv.Itoa(wallet.ID) // wallet-#
		}
		err = db.Save(wallet) // update database with complete wallet information
		if err != nil {
			return err
		}

		err = wallet.CreateSPVWallet([]byte(password), encryptedSeed, mw.btcWallet.btcDataDir)
		if err != nil {
			return fmt.Errorf("Create BTC wallet error: %v", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return wallet, nil
}

func (mw *MultiWallet) ConnectBTCSPVWallets() error {
	ctx, _ := mw.contextWithShutdownCancel()
	var wg sync.WaitGroup
	for _, wall := range mw.btcWallet.wallets {
		err := wall.ConnectSPVWallet(ctx, &wg)
		if err != nil {
			return err
		}
	}
	wg.Wait()
	return nil
}
