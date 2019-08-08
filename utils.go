package dcrlibwallet

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/decred/dcrd/dcrutil"
	"math"
	"os"
	"os/signal"
)

const (
	// Approximate time (in seconds) to mine a block in mainnet
	MainNetTargetTimePerBlock = 300

	// Approximate time (in seconds) to mine a block in testnet
	TestNetTargetTimePerBlock = 120

	// Use 10% of estimated total headers fetch time to estimate rescan time
	RescanPercentage = 0.1

	// Use 80% of estimated total headers fetch time to estimate address discovery time
	DiscoveryPercentage = 0.8

	MaxAmountAtom = dcrutil.MaxAmount
	MaxAmountDcr  = dcrutil.MaxAmount / dcrutil.AtomsPerCoin
)

func shutdownListener() {
	interruptChannel := make(chan os.Signal, 1)
	signal.Notify(interruptChannel, signals...)

	// Listen for the initial shutdown signal
	select {
	case sig := <-interruptChannel:
		log.Infof("Received signal (%s).  Shutting down...", sig)
	case <-shutdownRequestChannel:
		log.Info("Shutdown requested.  Shutting down...")
	}

	// Cancel all contexts created from withShutdownCancel.
	close(shutdownSignaled)

	// Listen for any more shutdown signals and log that shutdown has already
	// been signaled.
	for {
		select {
		case <-interruptChannel:
		case <-shutdownRequestChannel:
		}
		log.Info("Shutdown signaled.  Already shutting down...")
	}
}

func (lw *LibWallet) listenForShutdown() {

	lw.cancelFuncs = make([]context.CancelFunc, 0)
	lw.shuttingDown = make(chan bool)
	go func() {
		<-lw.shuttingDown
		for _, cancel := range lw.cancelFuncs {
			cancel()
		}
	}()
}

func contextWithShutdownCancel(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-shutdownSignaled
		cancel()
	}()
	return ctx, cancel
}

func calculateTotalTimeRemaining(timeRemainingInSeconds int64) string {
	minutes := timeRemainingInSeconds / 60
	if minutes > 0 {
		return fmt.Sprintf("%d min", minutes)
	}
	return fmt.Sprintf("%d sec", timeRemainingInSeconds)
}

func roundUp(n float64) int32 {
	return int32(math.Round(n))
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
