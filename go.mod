module github.com/raedahgroup/mobilewallet

require (
	github.com/AndreasBriese/bbloom v0.0.0-20180913140656-343706a395b7 // indirect
	github.com/decred/dcrd/addrmgr v1.0.2
	github.com/decred/dcrd/blockchain/stake v1.0.2
	github.com/decred/dcrd/chaincfg v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/connmgr v1.0.1
	github.com/decred/dcrd/dcrec v0.0.0-20181101015009-5f8081761da8
	github.com/decred/dcrd/dcrjson v1.0.0
	github.com/decred/dcrd/dcrutil v1.1.1
	github.com/decred/dcrd/hdkeychain v1.1.0
	github.com/decred/dcrd/rpcclient v1.0.2
	github.com/decred/dcrd/txscript v1.0.1
	github.com/decred/dcrd/wire v1.1.0
	github.com/decred/dcrwallet v1.2.2
	github.com/decred/dcrwallet/chain v1.0.1-0.20181109211527-ca582da21c08
	github.com/decred/dcrwallet/errors v1.0.0
	github.com/decred/dcrwallet/p2p v1.0.0
	github.com/decred/dcrwallet/spv v1.0.0
	github.com/decred/dcrwallet/ticketbuyer v1.0.1
	github.com/decred/dcrwallet/ticketbuyer/v2 v2.0.0
	github.com/decred/dcrwallet/wallet v1.0.0
	github.com/decred/dcrwallet/walletseed v1.0.0
	github.com/decred/slog v1.0.0
	github.com/dgraph-io/badger v1.5.4
	github.com/dgryski/go-farm v0.0.0-20180109070241-2de33835d102 // indirect
	github.com/jrick/logrotate v1.0.0
	github.com/pkg/errors v0.8.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/testify v1.2.2 // indirect
)

replace github.com/decred/dcrwallet/wallet => github.com/decred/dcrwallet/wallet v1.0.1-0.20181120205657-8690f1096aa7

replace github.com/decred/dcrwallet/spv => github.com/decred/dcrwallet/spv v1.0.1-0.20181120205657-8690f1096aa7

replace github.com/decred/dcrwallet => github.com/decred/dcrwallet v1.2.3-0.20181120205657-8690f1096aa7
