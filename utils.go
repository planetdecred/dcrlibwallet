package dcrlibwallet

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/wallet"
	"decred.org/dcrwallet/v2/wallet/txrules"
	"decred.org/dcrwallet/v2/walletseed"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/planetdecred/dcrlibwallet/internal/loader"
)

const (
	walletDbName = "wallet.db"

	// FetchPercentage is used to increase the initial estimate gotten during cfilters stage
	FetchPercentage = 0.38

	// Use 10% of estimated total headers fetch time to estimate rescan time
	RescanPercentage = 0.1

	// Use 80% of estimated total headers fetch time to estimate address discovery time
	DiscoveryPercentage = 0.8

	MaxAmountAtom = dcrutil.MaxAmount
	MaxAmountDcr  = dcrutil.MaxAmount / dcrutil.AtomsPerCoin

	TestnetHDPath       = "m / 44' / 1' / "
	LegacyTestnetHDPath = "m / 44’ / 11’ / "
	MainnetHDPath       = "m / 44' / 42' / "
	LegacyMainnetHDPath = "m / 44’ / 20’ / "

	DefaultRequiredConfirmations = 2
)

func (mw *MultiWallet) RequiredConfirmations() int32 {
	spendUnconfirmed := mw.ReadBoolConfigValueForKey(SpendUnconfirmedConfigKey, false)
	if spendUnconfirmed {
		return 0
	}
	return DefaultRequiredConfirmations
}

func (wallet *Wallet) RequiredConfirmations() int32 {
	var spendUnconfirmed bool
	wallet.readUserConfigValue(true, SpendUnconfirmedConfigKey, &spendUnconfirmed)
	if spendUnconfirmed {
		return 0
	}
	return DefaultRequiredConfirmations
}

func (mw *MultiWallet) listenForShutdown() {

	mw.cancelFuncs = make([]context.CancelFunc, 0)
	mw.shuttingDown = make(chan bool)
	go func() {
		<-mw.shuttingDown
		for _, cancel := range mw.cancelFuncs {
			cancel()
		}
	}()
}

func (wallet *Wallet) shutdownContextWithCancel() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	wallet.cancelFuncs = append(wallet.cancelFuncs, cancel)
	return ctx, cancel
}

func (wallet *Wallet) shutdownContext() (ctx context.Context) {
	ctx, _ = wallet.shutdownContextWithCancel()
	return
}

func (mw *MultiWallet) contextWithShutdownCancel() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	mw.cancelFuncs = append(mw.cancelFuncs, cancel)
	return ctx, cancel
}

func (mw *MultiWallet) ValidateExtPubKey(extendedPubKey string) error {
	_, err := hdkeychain.NewKeyFromString(extendedPubKey, mw.chainParams)
	if err != nil {
		if err == hdkeychain.ErrInvalidChild {
			return errors.New(ErrUnusableSeed)
		}

		return errors.New(ErrInvalid)
	}

	return nil
}

func NormalizeAddress(addr string, defaultPort string) (string, error) {
	// If the first SplitHostPort errors because of a missing port and not
	// for an invalid host, add the port.  If the second SplitHostPort
	// fails, then a port is not missing and the original error should be
	// returned.
	host, port, origErr := net.SplitHostPort(addr)
	if origErr == nil {
		return net.JoinHostPort(host, port), nil
	}
	addr = net.JoinHostPort(addr, defaultPort)
	_, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", origErr
	}
	return addr, nil
}

// For use with gomobile bind,
// doesn't support the alternative `GenerateSeed` function because it returns more than 2 types.
func GenerateSeed() (string, error) {
	seed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
	if err != nil {
		return "", err
	}

	return walletseed.EncodeMnemonic(seed), nil
}

func VerifySeed(seedMnemonic string) bool {
	_, err := walletseed.DecodeUserInput(seedMnemonic)
	return err == nil
}

// ExtractDateOrTime returns the date represented by the timestamp as a date string if the timestamp is over 24 hours ago.
// Otherwise, the time alone is returned as a string.
func ExtractDateOrTime(timestamp int64) string {
	utcTime := time.Unix(timestamp, 0).UTC()
	if time.Now().UTC().Sub(utcTime).Hours() > 24 {
		return utcTime.Format("2006-01-02")
	} else {
		return utcTime.Format("15:04:05")
	}
}

func FormatUTCTime(timestamp int64) string {
	return time.Unix(timestamp, 0).UTC().Format("2006-01-02 15:04:05")
}

func AmountCoin(amount int64) float64 {
	return dcrutil.Amount(amount).ToCoin()
}

func AmountAtom(f float64) int64 {
	amount, err := dcrutil.NewAmount(f)
	if err != nil {
		log.Error(err)
		return -1
	}
	return int64(amount)
}

func EncodeHex(hexBytes []byte) string {
	return hex.EncodeToString(hexBytes)
}

func EncodeBase64(text []byte) string {
	return base64.StdEncoding.EncodeToString(text)
}

func DecodeBase64(base64Text string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(base64Text)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func ShannonEntropy(text string) (entropy float64) {
	if text == "" {
		return 0
	}
	for i := 0; i < 256; i++ {
		px := float64(strings.Count(text, string(byte(i)))) / float64(len(text))
		if px > 0 {
			entropy += -px * math.Log2(px)
		}
	}
	return entropy
}

func TransactionDirectionName(direction int32) string {
	switch direction {
	case TxDirectionSent:
		return "Sent"
	case TxDirectionReceived:
		return "Received"
	case TxDirectionTransferred:
		return "Yourself"
	default:
		return "invalid"
	}
}

func CalculateTotalTimeRemaining(timeRemainingInSeconds int64) string {
	minutes := timeRemainingInSeconds / 60
	if minutes > 0 {
		return fmt.Sprintf("%d min", minutes)
	}
	return fmt.Sprintf("%d sec", timeRemainingInSeconds)
}

func CalculateDaysBehind(lastHeaderTime int64) string {
	diff := time.Since(time.Unix(lastHeaderTime, 0))
	daysBehind := int(math.Round(diff.Hours() / 24))
	if daysBehind == 0 {
		return "<1 day"
	} else if daysBehind == 1 {
		return "1 day"
	} else {
		return fmt.Sprintf("%d days", daysBehind)
	}
}

func StringsToHashes(h []string) ([]*chainhash.Hash, error) {
	hashes := make([]*chainhash.Hash, 0, len(h))
	for _, v := range h {
		hash, err := chainhash.NewHashFromStr(v)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	return hashes, nil
}

func roundUp(n float64) int32 {
	return int32(math.Round(n))
}

func WalletUniqueConfigKey(walletID int, key string) string {
	return fmt.Sprintf("%d%s", walletID, key)
}

func WalletExistsAt(directory string) bool {
	walletDbFilePath := filepath.Join(directory, walletDbName)
	exists, err := fileExists(walletDbFilePath)
	if err != nil {
		log.Errorf("wallet exists check error: %v", err)
	}
	return exists
}

func fileExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func moveFile(sourcePath, destinationPath string) error {
	if exists, _ := fileExists(sourcePath); exists {
		return os.Rename(sourcePath, destinationPath)
	}
	return nil
}

// done returns whether the context's Done channel was closed due to
// cancellation or exceeded deadline.
func done(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func backupFile(fileName string, suffix int) (newName string, err error) {
	newName = fileName + ".bak" + strconv.Itoa(suffix)
	exists, err := fileExists(newName)
	if err != nil {
		return "", err
	} else if exists {
		return backupFile(fileName, suffix+1)
	}

	err = moveFile(fileName, newName)
	if err != nil {
		return "", err
	}

	return newName, nil
}

func initWalletLoader(chainParams *chaincfg.Params, walletDataDir, walletDbDriver string) *loader.Loader {
	stakeOptions := &loader.StakeOptions{
		VotingEnabled: false,
		AddressReuse:  false,
		VotingAddress: nil,
	}

	defaultFeePerKb := txrules.DefaultRelayFeePerKb
	walletLoader := loader.NewLoader(chainParams, walletDataDir, stakeOptions, 20, false,
		defaultFeePerKb, wallet.DefaultAccountGapLimit, false)

	if walletDbDriver != "" {
		walletLoader.SetDatabaseDriver(walletDbDriver)
	}

	return walletLoader
}

func s(x float64) string {
	if int(x) == 1 {
		return ""
	}
	return "s"
}

// TimeElapsed returns the formatted time diffrence between two times as a string.
// If the argument `fullTime` is set to true, then the full time availabe is returned e.g 3 hours, 2 minutes, 20 seconds ago, 
// as opposed to 3 hours ago.
// If the argument `abbreviationFormat` is set to `long` the time format is e.g 2 minutes
// If the argument `abbreviationFormat` is set to `short` the time format is e.g 2 mins
// If the argument `abbreviationFormat` is set to `shortest` the time format is e.g 2 m
func TimeElapsed(now, then time.Time, abbreviationFormat string, fullTime bool) string {
	var parts []string
	var text string

	year2, month2, day2 := now.Date()
	hour2, minute2, second2 := now.Clock()

	year1, month1, day1 := then.Date()
	hour1, minute1, second1 := then.Clock()

	year := math.Abs(float64(year2 - year1))
	month := math.Abs(float64(month2 - month1))
	day := math.Abs(float64(day2 - day1))
	hour := math.Abs(float64(hour2 - hour1))
	minute := math.Abs(float64(minute2 - minute1))
	second := math.Abs(float64(second2 - second1))

	week := math.Floor(day / 7)

	if year > 0 {
		if abbreviationFormat == "long" {
			parts = append(parts, strconv.Itoa(int(year)) + " year" + s(year))
		} else if abbreviationFormat == "short" {
			parts = append(parts, strconv.Itoa(int(year)) + " yr" + s(year))
		} else if abbreviationFormat == "shortest" {
			parts = append(parts, strconv.Itoa(int(year)) + " y")
		}
	}

	if month > 0 {
		if abbreviationFormat == "long" {
			parts = append(parts, strconv.Itoa(int(month)) + " month" + s(month))
		} else if abbreviationFormat == "short" {
			parts = append(parts, strconv.Itoa(int(month)) + " mon" + s(month))
		} else if abbreviationFormat == "shortest" {
			parts = append(parts, strconv.Itoa(int(month)) + " m")
		}
	}

	if week > 0 {
		if abbreviationFormat == "long" {
			parts = append(parts, strconv.Itoa(int(week)) + " week" + s(week))
		} else if abbreviationFormat == "short" {
			parts = append(parts, strconv.Itoa(int(week)) + " wk" + s(week))
		} else if abbreviationFormat == "shortest" {
			parts = append(parts, strconv.Itoa(int(week)) + " w")
		}
	}

	if day > 0 {
		if abbreviationFormat == "long" {
			parts = append(parts, strconv.Itoa(int(day)) + " day" + s(day))
		} else if abbreviationFormat == "short" {
			parts = append(parts, strconv.Itoa(int(day)) + " dy" + s(day))
		} else if abbreviationFormat == "shortest" {
			parts = append(parts, strconv.Itoa(int(day)) + " d")
		}
	}

	if hour > 0 {
		if abbreviationFormat == "long" {
			parts = append(parts, strconv.Itoa(int(hour)) + " hour" + s(hour))
		} else if abbreviationFormat == "short" {
			parts = append(parts, strconv.Itoa(int(hour)) + " hr" + s(hour))
		} else if abbreviationFormat == "shortest" {
			parts = append(parts, strconv.Itoa(int(hour)) + " h")
		}
	}

	if minute > 0 {
		if abbreviationFormat == "long" {
			parts = append(parts, strconv.Itoa(int(minute)) + " minute" + s(minute))
		} else if abbreviationFormat == "short" {
			parts = append(parts, strconv.Itoa(int(minute)) + " min" + s(minute))
		} else if abbreviationFormat == "shortest" {
			parts = append(parts, strconv.Itoa(int(minute)) + " mi")
		}
	}

	if second > 0 {
		if abbreviationFormat == "long" {
			parts = append(parts, strconv.Itoa(int(second)) + " second" + s(second))
		} else if abbreviationFormat == "short" {
			parts = append(parts, strconv.Itoa(int(second)) + " sec" + s(second))
		} else if abbreviationFormat == "shortest" {
			parts = append(parts, strconv.Itoa(int(second)) + " s")
		}
	}

	if now.After(then) {
		text = " ago"
	} else {
		text = " after"
	}

	if len(parts) == 0 {
		return "just now"
	}

	if fullTime {
		return strings.Join(parts, ", ") + text
	}
	return parts[0] + text
}
