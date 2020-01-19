package dcrlibwallet_test

import (
	"os"
	"testing"
	"testing/quick"

	. "github.com/raedahgroup/dcrlibwallet"
)

// canUseDir returns true if the program can create the directory
// if it doesn't exist or the directory is empty if it exists.
// If will delete the directory if it is empty.
func canUseDir(directory string) bool {
	if os.MkdirAll(directory, os.ModePerm) == nil {
		return os.Remove(directory) != nil
	}
	return false
}

// TestNewMultiWallet tests if NewMultiWallet will return an error for valid
// paths or not return an error for invalid paths
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
