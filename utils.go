package dcrlibwallet

const (
	TestnetJSONRPCClientPort = "19109"
	MainnetJSONRPCClientPort = "9109"
)

func rpcClientDefaultPortForNet(netype string) string {
	if netype == "mainnet" {
		return MainnetJSONRPCClientPort
	}

	return TestnetJSONRPCClientPort
}
