module github.com/planetdecred/dcrlibwallet

require (
	decred.org/dcrwallet/v2 v2.0.0-20210923184553-4f3b2d70ea25
	github.com/DataDog/zstd v1.4.8 // indirect
	github.com/asdine/storm v0.0.0-20190216191021-fe89819f6282
	github.com/decred/dcrd/addrmgr/v2 v2.0.0-20210914212651-723d86274b0d
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0-20210914212651-723d86274b0d
	github.com/decred/dcrd/chaincfg/chainhash v1.0.4-0.20210914212651-723d86274b0d
	github.com/decred/dcrd/chaincfg/v3 v3.0.1-0.20210914212651-723d86274b0d
	github.com/decred/dcrd/connmgr/v3 v3.0.1-0.20210914212651-723d86274b0d
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2-0.20210525214639-70483c835b7f // indirect
	github.com/decred/dcrd/dcrutil/v4 v4.0.0-20210914212651-723d86274b0d
	github.com/decred/dcrd/gcs/v3 v3.0.0-20210914212651-723d86274b0d
	github.com/decred/dcrd/hdkeychain/v3 v3.0.1-0.20210914212651-723d86274b0d
	github.com/decred/dcrd/txscript/v4 v4.0.0-20210914212651-723d86274b0d
	github.com/decred/dcrd/wire v1.4.1-0.20210914212651-723d86274b0d
	github.com/decred/dcrdata/v7 v7.0.0-20211002170506-a6280ab3bef3
	github.com/decred/politeia v1.0.1
	github.com/decred/slog v1.2.0
	github.com/dgraph-io/badger v1.6.2
	github.com/jrick/logrotate v1.0.0
	github.com/kevinburke/nacl v0.0.0-20190829012316-f3ed23dbd7f8
	github.com/onsi/ginkgo v1.14.0
	github.com/onsi/gomega v1.10.1
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83
	golang.org/x/sync v0.0.0-20200625203802-6e8e738ad208
)

go 1.16
