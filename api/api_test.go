package api

import (
	"reflect"
	"testing"
)

func TestGetBestBlock(t *testing.T) {
	service := NewService()
	height := service.GetBestBlock()
	if reflect.TypeOf(height).Kind() != reflect.Int32 {
		t.Errorf("api.GetBestBlock returned %v; want unit32", reflect.TypeOf(height))
	}

	if height == -1 {
		t.Errorf("api.GetBestBlock = %d; want uint > 1", height)
	}
}
