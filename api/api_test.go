package api

import (
	"reflect"
	"testing"

	"github.com/planetdecred/dcrlibwallet/utils"
)

func TestGetBestBlock(t *testing.T) {
	chainParams, err := utils.ChainParams("mainnet")
	if err != nil {
		log.Error("Error creating chain params.")
		return
	}

	service := NewService(chainParams)
	height := service.GetBestBlock()
	if reflect.TypeOf(height).Kind() != reflect.Int32 {
		t.Errorf("api.GetBestBlock returned %v; want unit32", reflect.TypeOf(height))
	}

	if height == -1 {
		t.Errorf("api.GetBestBlock = %d; want uint > 1", height)
	}
}
