package dcrlibwallet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/netparams"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/raedahgroup/dcrlibwallet/addresshelper"
	"github.com/raedahgroup/dcrlibwallet/txindex"
	"github.com/raedahgroup/dcrlibwallet/utils"
)

var (
	shutdownRequestChannel = make(chan struct{})
	shutdownSignaled       = make(chan struct{})
	signals                = []os.Signal{os.Interrupt, syscall.SIGTERM}
)

const logFileName = "dcrlibwallet.log"

type LibWallet struct {
	walletDataDir          string
	activeNet              *netparams.Params
	walletLoader           *WalletLoader
	wallet                 *wallet.Wallet
	txIndexDB              *txindex.DB
	txNotificationListener TransactionListener
	cancelFuncs            []context.CancelFunc
	shuttingDown           chan bool
	*syncData
}

func NewLibWallet(homeDir string, dbDriver string, netType string) (*LibWallet, error) {
	activeNet := utils.NetParams(netType)
	if activeNet == nil {
		return nil, fmt.Errorf("unsupported network type: %s", netType)
	}

	walletDataDir := filepath.Join(homeDir, activeNet.Name)
	return newLibWallet(walletDataDir, dbDriver, activeNet, true)
}

// newLibWallet creates a LibWallet
func newLibWallet(walletDataDir, walletDbDriver string, activeNet *netparams.Params, listenForShutdown bool) (*LibWallet, error) {
	errors.Separator = ":: "
	initLogRotator(filepath.Join(walletDataDir, logFileName))

	// init walletLoader
	stakeOptions := &StakeOptions{
		VotingEnabled: false,
		AddressReuse:  false,
		VotingAddress: nil,
		TicketFee:     txrules.DefaultRelayFeePerKb.ToCoin(),
	}

	walletLoader := NewLoader(activeNet.Params, walletDataDir, stakeOptions, 20, false,
		txrules.DefaultRelayFeePerKb.ToCoin(), wallet.DefaultAccountGapLimit)
	walletLoader.SetDatabaseDriver(walletDbDriver)

	if listenForShutdown {
		go shutdownListener()
	}

	lw := &LibWallet{
		walletDataDir: walletDataDir,
		activeNet:     activeNet,
		walletLoader:  walletLoader,
		syncData:      &syncData{},
	}

	return lw, nil
}

//Shutdown closes a wallet instance
func (lw *LibWallet) Shutdown(exit bool) {
	log.Info("Shutting down mobile wallet")

	if lw.rpcClient != nil {
		lw.rpcClient.Stop()
	}

	close(shutdownSignaled)

	if lw.cancelSync != nil {
		lw.cancelSync()
	}

	if logRotator != nil {
		log.Infof("Shutting down log rotator")
		logRotator.Close()
	}

	if _, loaded := lw.walletLoader.LoadedWallet(); loaded {
		err := lw.walletLoader.UnloadWallet()
		if err != nil {
			log.Errorf("Failed to close wallet: %v", err)
		} else {
			log.Infof("Closed wallet")
		}
	}

	if lw.txIndexDB != nil {
		err := lw.txIndexDB.Close()
		if err != nil {
			log.Errorf("tx db closed with error: %v", err)
		} else {
			log.Info("tx db closed successfully")
		}
	}

	if exit {
		os.Exit(0)
	}
}

// SignMessage returns the signature of a signed message
// using an 'address' associated private key.
func (lw *LibWallet) SignMessage(passphrase []byte, address string, message string) ([]byte, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{}
	}()
	err := lw.wallet.Unlock(passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	addr, err := addresshelper.DecodeForNetwork(address, lw.activeNet.Params)
	if err != nil {
		return nil, translateError(err)
	}

	var sig []byte
	switch a := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
	case *dcrutil.AddressPubKeyHash:
		if a.DSA(a.Net()) != dcrec.STEcdsaSecp256k1 {
			return nil, errors.New(ErrInvalidAddress)
		}
	default:
		return nil, errors.New(ErrInvalidAddress)
	}

	sig, err = lw.wallet.SignMessage(message, addr)
	if err != nil {
		return nil, translateError(err)
	}

	return sig, nil
}

// VerifyMessage verifies that sig is a valid signature of msg and was created
// using the secp256k1 private key for addr.
func (lw *LibWallet) VerifyMessage(address string, message string, signatureBase64 string) (bool, error) {
	var valid bool

	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		return false, translateError(err)
	}

	signature, err := utils.DecodeBase64(signatureBase64)
	if err != nil {
		return false, err
	}

	// Addresses must have an associated secp256k1 private key and therefore
	// must be P2PK or P2PKH (P2SH is not allowed).
	switch a := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
	case *dcrutil.AddressPubKeyHash:
		if a.DSA(a.Net()) != dcrec.STEcdsaSecp256k1 {
			return false, errors.New(ErrInvalidAddress)
		}
	default:
		return false, errors.New(ErrInvalidAddress)
	}

	valid, err = wallet.VerifyMessage(message, addr, signature)
	if err != nil {
		return false, translateError(err)
	}

	return valid, nil
}

//CallJSONRPC attempts to make a request to an RPC server using
// the user specified connection configurations.
// If request is successful it returns a result
func (lw *LibWallet) CallJSONRPC(method string, args string, address string, username string, password string, caCert string) (string, error) {
	arguments := strings.Split(args, ",")
	params := make([]interface{}, 0)
	for _, arg := range arguments {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		params = append(params, strings.TrimSpace(arg))
	}
	// Attempt to create the appropriate command using the arguments
	// provided by the user.
	cmd, err := dcrjson.NewCmd(method, params...)
	if err != nil {
		// Show the error along with its error code when it's a
		// dcrjson.Error as it reallistcally will always be since the
		// NewCmd function is only supposed to return errors of that
		// type.
		if jerr, ok := err.(dcrjson.Error); ok {
			log.Errorf("%s command: %v (code: %s)\n",
				method, err, jerr.Code)
			return "", err
		}
		// The error is not a dcrjson.Error and this really should not
		// happen.  Nevertheless, fallback to just showing the error
		// if it should happen due to a bug in the package.
		log.Errorf("%s command: %v\n", method, err)
		return "", err
	}

	// Marshal the command into a JSON-RPC byte slice in preparation for
	// sending it to the RPC server.
	marshalledJSON, err := dcrjson.MarshalCmd("1.0", 1, cmd)
	if err != nil {
		log.Error(err)
		return "", err
	}

	// Send the JSON-RPC request to the server using the user-specified
	// connection configuration.
	result, err := utils.SendPostRequest(marshalledJSON, address, username, password, caCert)
	if err != nil {
		log.Error(err)
		return "", err
	}

	// Choose how to display the result based on its type.
	strResult := string(result)
	if strings.HasPrefix(strResult, "{") || strings.HasPrefix(strResult, "[") {
		var dst bytes.Buffer
		if err := json.Indent(&dst, result, "", "  "); err != nil {
			log.Errorf("Failed to format result: %v", err)
			return "", err
		}
		fmt.Println(dst.String())
		return dst.String(), nil

	} else if strings.HasPrefix(strResult, `"`) {
		var str string
		if err := json.Unmarshal(result, &str); err != nil {
			log.Errorf("Failed to unmarshal result: %v", err)
			return "", err
		}
		fmt.Println(str)
		return str, nil

	} else if strResult != "null" {
		fmt.Println(strResult)
		return strResult, nil
	}
	return "", nil
}
