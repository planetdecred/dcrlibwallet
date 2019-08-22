module github.com/raedahgroup/dcrlibwallet

require (
	github.com/AndreasBriese/bbloom v0.0.0-20190306092124-e2d15f34fcf9 // indirect
	github.com/DataDog/zstd v1.3.5 // indirect
	github.com/asdine/storm v0.0.0-20190216191021-fe89819f6282
	github.com/decred/dcrd/addrmgr v1.0.2
	github.com/decred/dcrd/blockchain/stake v1.2.1
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.1
	github.com/decred/dcrd/chaincfg v1.5.2
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.2.0
	github.com/decred/dcrd/connmgr v1.0.2
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrjson v1.2.0
	github.com/decred/dcrd/dcrutil v1.4.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.0
	github.com/decred/dcrd/gcs v1.1.0
	github.com/decred/dcrd/hdkeychain v1.1.1
	github.com/decred/dcrd/rpcclient v1.1.0
	github.com/decred/dcrd/txscript v1.1.0
	github.com/decred/dcrd/txscript/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/txhelpers v1.1.0
	github.com/decred/dcrwallet v1.2.2
	github.com/decred/dcrwallet/chain v1.0.0
	github.com/decred/dcrwallet/chain/v3 v3.0.0
	github.com/decred/dcrwallet/errors v1.1.0
	github.com/decred/dcrwallet/lru v1.0.0
	github.com/decred/dcrwallet/p2p/v2 v2.0.0
	github.com/decred/dcrwallet/spv/v3 v3.0.0 // indirect
	github.com/decred/dcrwallet/ticketbuyer v1.0.2
	github.com/decred/dcrwallet/ticketbuyer/v2 v2.0.1
	github.com/decred/dcrwallet/validate v1.0.2
	github.com/decred/dcrwallet/wallet/v2 v2.1.1
	github.com/decred/dcrwallet/wallet/v3 v3.0.0
	github.com/decred/dcrwallet/walletseed v1.0.1
	github.com/decred/slog v1.0.0
	github.com/dgraph-io/badger v1.5.4
	github.com/dgryski/go-farm v0.0.0-20190104051053-3adb47b1fb0f // indirect
	github.com/jrick/logrotate v1.0.0
	github.com/onsi/ginkgo v1.8.0 // indirect
	github.com/onsi/gomega v1.5.0 // indirect
	github.com/pkg/errors v0.8.1 // indirect
	github.com/stretchr/testify v1.3.0 // indirect
	go.etcd.io/bbolt v1.3.3
	golang.org/x/net v0.0.0-20190420063019-afa5a82059c6 // indirect
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	golang.org/x/sys v0.0.0-20190422165155-953cdadca894 // indirect
	google.golang.org/appengine v1.5.0 // indirect
)

replace (
	github.com/decred/dcrwallet => github.com/decred/dcrwallet v1.2.3-0.20190820143957-746bab522803
	github.com/decred/dcrwallet/chain/v3 => github.com/decred/dcrwallet/chain/v3 v3.0.0-20190820143957-746bab522803
	github.com/decred/dcrwallet/deployments/v2 => github.com/decred/dcrwallet/deployments/v2 v2.0.0-20190820143957-746bab522803
	github.com/decred/dcrwallet/p2p/v2 => github.com/decred/dcrwallet/p2p/v2 v2.0.0-20190820143957-746bab522803
	github.com/decred/dcrwallet/rpc/client/dcrd => github.com/decred/dcrwallet/rpc/client/dcrd v0.0.0-20190820143957-746bab522803
	github.com/decred/dcrwallet/spv/v3 => github.com/decred/dcrwallet/spv/v3 v3.0.0-20190820143957-746bab522803
	github.com/decred/dcrwallet/ticketbuyer/v4 => github.com/decred/dcrwallet/ticketbuyer/v4 v4.0.0-20190820143957-746bab522803
	github.com/decred/dcrwallet/wallet/v3 => github.com/decred/dcrwallet/wallet/v3 v3.0.0-20190820143957-746bab522803
)
