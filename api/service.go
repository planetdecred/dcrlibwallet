package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	apiTypes "github.com/decred/dcrdata/v7/api/types"
)

type Service struct {
	client *Client
}

func NewService() *Service {
	conf := &ClientConf{
		BaseUrl: "https://explorer.planetdecred.org/",
		Debug:   false,
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

// GetBestBlock returns the best block height as int32.
func (s *Service) GetBestBlock() int32 {
	r, err := s.client.Do("GET", "api/block/best/height", "")
	if err != nil {
		fmt.Println(err)
		return -1
	}

	h, _ := strconv.ParseInt(string(r), 10, 32)
	return int32(h)
}

// GetBestBlockTimeStamp returns best block time, as unix timestamp.
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

// GetTransactionRaw
/*func (s *Service) GetTransactionRaw(txHash string) (*Transaction, error) {
	r, err := s.client.Do("GET", "insight/api/tx/"+txHash, "")
	if err != nil {
		fmt.Println(err)
		return -1
	}
	return
}*/

// GetCurrentAgendaStatus returns the current agenda and its status.
func (s *Service) GetCurrentAgendaStatus() (agenda *chainjson.GetVoteInfoResult, err error) {
	r, err := s.client.Do("GET", "api/stake/vote/info", "")
	if err != nil {
		fmt.Println(err)
		return agenda, err
	}
	err = json.Unmarshal(r, &agenda)
	if err != nil {
		return
	}
	fmt.Printf("Agenda: %+v \n", agenda)
	return
}

// GetAgendas returns all agendas high level details
func (s *Service) GetAgendas() (agendas []apiTypes.AgendasInfo, err error) {
	r, err := s.client.Do("GET", "api/agendas", "")
	if err != nil {
		fmt.Println(err)
		return
	}
	err = json.Unmarshal(r, &agendas)
	if err != nil {
		return
	}
	return
}

// GetAgendaDetails returns the details for agenda with agendaId
func (s *Service) GetAgendaDetails(agendaId string) (agendaDetails *apiTypes.AgendaAPIResponse, err error) {
	r, err := s.client.Do("GET", "api/agenda/"+agendaId, "")
	if err != nil {
		fmt.Println(err)
		return
	}
	err = json.Unmarshal(r, &agendaDetails)
	if err != nil {
		return
	}
	return
}

// GetTreasuryBalance returns the current treasury balance as int64.
func (s *Service) GetTreasuryBalance() (bal int64, err error) {
	bal = -1
	r, err := s.client.Do("GET", "api/treasury/balance", "")
	if err != nil {
		fmt.Println(err)
		return
	}

	treasury := &TreasuryDetails{}
	err = json.Unmarshal(r, treasury)
	if err != nil {
		return
	}
	bal = treasury.Balance
	return
}

// GetTreasuryDetails the current tresury balance, spent amount, added amount, and tx count for the
// treasury.
func (s *Service) GetTreasuryDetails() (treasuryDetails *TreasuryDetails, err error) {
	r, err := s.client.Do("GET", "api/treasury/balance", "")
	if err != nil {
		fmt.Println(err)
		return
	}

	err = json.Unmarshal(r, &treasuryDetails)
	if err != nil {
		return
	}
	return
}
