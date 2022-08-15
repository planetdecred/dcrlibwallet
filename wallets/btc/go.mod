module github.com/planetdecred/dcrlibwallet/wallets/btc

require (
	decred.org/dcrwallet/v2 v2.0.2-0.20220505152146-ece5da349895
	github.com/asdine/storm v0.0.0-20190216191021-fe89819f6282
	github.com/btcsuite/btcd v0.22.1
	github.com/btcsuite/btcd/btcutil v1.1.1
	github.com/btcsuite/btcd/chaincfg/chainhash v1.0.1
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v1.0.3-0.20210527170813-e2ba6805a890 // note: hoists btcd's own require of btcutil
	github.com/btcsuite/btcwallet v0.12.0
	github.com/btcsuite/btcwallet/walletdb v1.4.0
	github.com/btcsuite/btcwallet/wtxmgr v1.3.0
	github.com/decred/dcrd/addrmgr/v2 v2.0.0 // indirect
	github.com/decred/dcrd/connmgr/v3 v3.1.0 // indirect
	github.com/decred/slog v1.2.0
	github.com/jrick/logrotate v1.0.0
	github.com/kevinburke/nacl v0.0.0-20190829012316-f3ed23dbd7f8 // indirect
	github.com/lightninglabs/neutrino v0.13.1-0.20211214231330-53b628ce1756
	github.com/planetdecred/dcrlibwallet v1.7.0 // indirect
	golang.org/x/crypto v0.0.0-20220427172511-eb4f295cb31f // indirect
)

go 1.16
