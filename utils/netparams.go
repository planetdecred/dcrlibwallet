package utils

import (
	"strings"

	"github.com/decred/dcrd/chaincfg/v2"
)

func NetParams(netType string) *chaincfg.Params {
	switch strings.ToLower(netType) {
	case strings.ToLower(chaincfg.MainNetParams().Name):
		return chaincfg.MainNetParams()
	case strings.ToLower(chaincfg.TestNet3Params().Name):
		return chaincfg.TestNet3Params()
	default:
		return nil
	}
}
