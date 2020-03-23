module github.com/raedahgroup/dcrlibwallet

require (
	github.com/asdine/storm v0.0.0-20190216191021-fe89819f6282
	github.com/decred/dcrd/addrmgr v1.1.0
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.2
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/connmgr/v2 v2.0.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/hdkeychain/v2 v2.1.0
	github.com/decred/dcrd/txscript/v2 v2.1.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrwallet/errors v1.1.0
	github.com/decred/dcrwallet/errors/v2 v2.0.0
	github.com/decred/dcrwallet/p2p/v2 v2.0.0
	github.com/decred/dcrwallet/rpc/client/dcrd v1.0.0
	github.com/decred/dcrwallet/ticketbuyer/v4 v4.0.0
	github.com/decred/dcrwallet/wallet/v3 v3.2.1-badger
	github.com/decred/dcrwallet/walletseed v1.0.1
	github.com/decred/slog v1.0.0
	github.com/dgraph-io/badger v1.5.4
	github.com/jrick/logrotate v1.0.0
	github.com/raedahgroup/dcrlibwallet/spv v0.0.0-00010101000000-000000000000
	github.com/raedahgroup/godcr-gio v0.0.0-20200315232334-39ec94c7c23b // indirect
	github.com/raedahgroup/godcr/app v0.0.0-20200113191321-d41651a66ae3 // indirect
	go.etcd.io/bbolt v1.3.3
	golang.org/x/crypto v0.0.0-20190820162420-60c769a6c586
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
)

replace (
	decred.org/dcrwallet => decred.org/dcrwallet v1.2.3-0.20191024200307-d273b5687adf
	github.com/decred/dcrwallet/wallet/v3 => github.com/raedahgroup/dcrwallet/wallet/v3 v3.2.1-badger
	github.com/raedahgroup/dcrlibwallet/spv => ./spv
)

go 1.13
