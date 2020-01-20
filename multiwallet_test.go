package dcrlibwallet_test

import (
	"os"
	"testing"
	"testing/quick"

	. "github.com/raedahgroup/dcrlibwallet"
)

// canUseDir returns true if the program can create the directory
// It will create the directory if it can
func canUseDir(directory string) bool {
	return os.MkdirAll(directory, os.ModePerm) == nil
}

// TestNewMultiWalletPath checks that NewMultiWallet returns an error for invalid
// paths or no error for valid paths
func TestNewMultiWallet(t *testing.T) {
	f := func(rootDir string) bool {
		canUse := canUseDir(rootDir)
		_, err := NewMultiWallet(rootDir, "", "testnet")
		if canUse {
			return err == nil
		}
		return err != nil
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}
