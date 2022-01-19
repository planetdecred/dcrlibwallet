module github.com/planetdecred/dcrlibwallet

require (
	decred.org/dcrdex v0.4.0-rc3
	decred.org/dcrwallet/v2 v2.0.0-rc3
	github.com/DataDog/zstd v1.4.8 // indirect
	github.com/asdine/storm v0.0.0-20190216191021-fe89819f6282
	github.com/decred/dcrd/addrmgr/v2 v2.0.0
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3
	github.com/decred/dcrd/chaincfg/v3 v3.1.1
	github.com/decred/dcrd/connmgr/v3 v3.1.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.0
	github.com/decred/dcrd/gcs/v3 v3.0.0
	github.com/decred/dcrd/hdkeychain/v3 v3.1.0
	github.com/decred/dcrd/txscript/v4 v4.0.0
	github.com/decred/dcrd/wire v1.5.0
	github.com/decred/dcrdata/v7 v7.0.0-20211216152310-365c9dc820eb
	github.com/decred/politeia v1.3.0
	github.com/decred/slog v1.2.0
	github.com/dgraph-io/badger v1.6.2
	github.com/jrick/logrotate v1.0.0
	github.com/kevinburke/nacl v0.0.0-20190829012316-f3ed23dbd7f8
	github.com/onsi/ginkgo v1.14.0
	github.com/onsi/gomega v1.10.1
	github.com/planetdecred/dcrlibwallet/dexdcr v0.0.0-20220111145554-3b9241786979
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
)

replace github.com/planetdecred/dcrlibwallet/dexdcr => github.com/itswisdomagain/dcrlibwallet/dexdcr v0.0.0-20220119152459-cdc29cf215c6

go 1.16
