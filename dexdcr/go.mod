module github.com/planetdecred/dcrlibwallet/dexdcr

require (
	decred.org/cspp v0.3.0 // indirect
	decred.org/dcrdex v0.0.0-20210929171239-72212a8bfa41
	decred.org/dcrwallet/v2 v2.0.0-20210923184553-4f3b2d70ea25
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/dchest/siphash v1.2.2 // indirect
	github.com/decred/base58 v1.0.3 // indirect
	github.com/decred/dcrd/addrmgr/v2 v2.0.0-20210914212651-723d86274b0d // indirect
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0-20210914212651-723d86274b0d
	github.com/decred/dcrd/blockchain/standalone/v2 v2.0.1-0.20210914212651-723d86274b0d
	github.com/decred/dcrd/chaincfg/chainhash v1.0.4-0.20210914212651-723d86274b0d
	github.com/decred/dcrd/chaincfg/v3 v3.0.1-0.20210914212651-723d86274b0d
	github.com/decred/dcrd/connmgr/v3 v3.0.1-0.20210914212651-723d86274b0d // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.1-0.20210914212651-723d86274b0d // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2-0.20210914212651-723d86274b0d // indirect
	github.com/decred/dcrd/database/v3 v3.0.0-20210914212651-723d86274b0d // indirect
	github.com/decred/dcrd/dcrec v1.0.1-0.20210914212651-723d86274b0d // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2-0.20210715032435-c9521b468f95 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1-0.20210914212651-723d86274b0d // indirect
	github.com/decred/dcrd/dcrjson/v4 v4.0.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.0-20210914212651-723d86274b0d
	github.com/decred/dcrd/gcs/v3 v3.0.0-20210914212651-723d86274b0d // indirect
	github.com/decred/dcrd/hdkeychain/v3 v3.0.1-0.20210914212651-723d86274b0d // indirect
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0-20210914212651-723d86274b0d
	github.com/decred/dcrd/rpcclient/v7 v7.0.0-20210914193033-2efb9bda71fe // indirect
	github.com/decred/dcrd/txscript/v4 v4.0.0-20210914212651-723d86274b0d
	github.com/decred/dcrd/wire v1.4.1-0.20210914212651-723d86274b0d
	github.com/decred/go-socks v1.1.0 // indirect
	github.com/decred/slog v1.2.0
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/jrick/bitset v1.0.0 // indirect
	github.com/jrick/wsrpc/v2 v2.3.4 // indirect
	github.com/planetdecred/dcrlibwallet v0.0.0-00010101000000-000000000000
	golang.org/x/crypto v0.0.0-20210322153248-0c34fe9e7dc2 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c // indirect
	golang.org/x/sys v0.0.0-20210903071746-97244b99971b // indirect
	gopkg.in/ini.v1 v1.55.0 // indirect
)

replace (
	decred.org/dcrdex => github.com/itswisdomagain/dcrdex v0.0.0-20211004141752-92a02cc7352a
	github.com/planetdecred/dcrlibwallet => ../
)

go 1.17
