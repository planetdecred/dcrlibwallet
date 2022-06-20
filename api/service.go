package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type Service struct {
	client *Client
}

func NewService() *Service {
	conf := &ClientConf{
		BaseUrl: "https://explorer.planetdecred.org/",
		Debug:   true,
	}

	client := NewClient(conf)
	client.ReqFilter = func(info RequestInfo) (req *http.Request, err error) {

		req, err = http.NewRequest(info.Method, info.Url, bytes.NewBuffer([]byte(info.Payload.(string))))
		if err != nil {
			return
		}
		if info.Method == "POST" || info.Method == "PUT" {
			req.Header.Add("Content-Type", "application/json;charset=utf-8")
		}
		req.Header.Add("Accept", "application/json")

		return
	}

	return &Service{
		client: client,
	}
}

func (s *Service) GetBestBlock() int32 {
	r, err := s.client.Do("GET", "api/block/best/height", "")
	if err != nil {
		fmt.Println(err)
		return -1
	}

	h, _ := strconv.ParseInt(string(r), 10, 32)
	return int32(h)
}

func (s *Service) GetBestBlockTimeStamp() int64 {
	r, err := s.client.Do("GET", "api/block/best?txtotals=false", "")
	if err != nil {
		fmt.Println(err)
		return -1
	}
	var blockDataBasic *BlockDataBasic
	err = json.Unmarshal(r, &blockDataBasic)
	if err != nil {
		fmt.Println(err)
		return -1
	}
	return blockDataBasic.Time.UNIX()
}

func (s *Service) GetTransactionRaw(txHash string) (*Transaction, error) {
	r, err := s.client.Do("GET", "insight/api/tx/"+txHash, "")
	if err != nil {
		fmt.Println(err)
		return -1
	}
	return
}
