package main

import (
	"fmt"

	dcrlibwallet "github.com/raedahgroup/dcrlibwallet"
)

func main() {
	multiWallet, err := dcrlibwallet.NewMultiWallet("/Users/collins/Library/Application Support/Dcrwallet/dcrlibwallet", "bdb", "testnet3")
	if err != nil {
		panic(err)
	}

	seed1, _ := dcrlibwallet.GenerateSeed()
	_, err = multiWallet.CreateNewWallet("default", "private", seed1)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created and Loaded wallet [default]\n")

	seed2, _ := dcrlibwallet.GenerateSeed()
	_, err = multiWallet.CreateNewWallet("wallet-2", "private", seed2)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created and Loaded wallet [wallet-2]\n")

	// err = multiWallet.OpenWallet("default", []byte("public"))
	// if err != nil {
	// 	panic(err)
	// }
	// err = multiWallet.OpenWallet("wallet-2", []byte("public"))
	// if err != nil {
	// 	panic(err)
	// }

	fmt.Println("Calling spv sync")
	multiWallet.SpvSync("127.0.0.1")
}
