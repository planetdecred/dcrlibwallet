package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/decred/dcrd/chaincfg/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	apiTypes "github.com/decred/dcrdata/v7/api/types"
)

const (
	mainnetBaseUrl = "https://mainnet.dcrdata.org/"
	testnetBaseUrl = "https://testnet.dcrdata.org/"
)

type Service struct {
	client      *Client
	chainParams *chaincfg.Params
}

func NewService(chainParams *chaincfg.Params) *Service {
	conf := &ClientConf{}
	if chainParams.Name == chaincfg.TestNet3Params().Name {
		conf.BaseUrl = testnetBaseUrl
	} else {
		conf.BaseUrl = mainnetBaseUrl
	}

	client := NewClient(conf)
	client.ReqFilter = func(info RequestInfo) (req *http.Request, err error) {

		req, err = http.NewRequest(info.Method, info.Url, bytes.NewBuffer([]byte(info.Payload.(string))))
		if err != nil {
			log.Error(err)
			return
		}
		if info.Method == "POST" || info.Method == "PUT" {
			req.Header.Add("Content-Type", "application/json;charset=utf-8")
		}
		req.Header.Add("Accept", "application/json")

		return
	}

	return &Service{
		client:      client,
		chainParams: chainParams,
	}
}

// GetBestBlock returns the best block height as int32.
func (s *Service) GetBestBlock() int32 {
	r, err := s.client.Do("GET", "api/block/best/height", "")
	if err != nil {
		return -1
	}

	h, _ := strconv.ParseInt(string(r), 10, 32)
	return int32(h)
}

// GetBestBlockTimeStamp returns best block time, as unix timestamp.
func (s *Service) GetBestBlockTimeStamp() int64 {
	r, err := s.client.Do("GET", "api/block/best?txtotals=false", "")
	if err != nil {
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

// GetCurrentAgendaStatus returns the current agenda and its status.
func (s *Service) GetCurrentAgendaStatus() (agenda *chainjson.GetVoteInfoResult, err error) {
	r, err := s.client.Do("GET", "api/stake/vote/info", "")
	if err != nil {
		return
	}
	err = json.Unmarshal(r, &agenda)
	if err != nil {
		return
	}
	return
}

// GetAgendas returns all agendas high level details
func (s *Service) GetAgendas() (agendas []apiTypes.AgendasInfo, err error) {
	r, err := s.client.Do("GET", "api/agendas", "")
	if err != nil {
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
		return
	}

	err = json.Unmarshal(r, &treasuryDetails)
	if err != nil {
		return
	}
	return
}

// GetExchangeRate fetches exchange rate data summary
func (s *Service) GetExchangeRate() (rates *ExchangeRates, err error) {
	r, err := s.client.Do("GET", "api/exchangerate", "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &rates)
	if err != nil {
		return
	}
	return
}

// GetExchanges fetches the current known state of all exchanges
func (s *Service) GetExchanges() (state *ExchangeState, err error) {
	r, err := s.client.Do("GET", "api/exchanges", "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &state)
	if err != nil {
		return
	}
	return
}

// GetTicketFeeRateSummary fetches the current known state of all exchanges
func (s *Service) GetTicketFeeRateSummary() (ticketInfo *apiTypes.MempoolTicketFeeInfo, err error) {
	r, err := s.client.Do("GET", "api/mempool/sstx", "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &ticketInfo)
	if err != nil {
		return
	}
	return
}

func (s *Service) GetTicketFeeRate() (ticketFeeRate *apiTypes.MempoolTicketFees, err error) {
	r, err := s.client.Do("GET", "api/mempool/sstx/fees", "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &ticketFeeRate)
	if err != nil {
		return
	}
	return
}

func (s *Service) GetNHighestTicketFeeRate(nHighest int) (ticketFeeRate *apiTypes.MempoolTicketFees, err error) {
	r, err := s.client.Do("GET", "api/mempool/sstx/fees/"+strconv.Itoa(nHighest), "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &ticketFeeRate)
	if err != nil {
		return
	}
	return
}

func (s *Service) GetTicketDetails() (ticketDetails *apiTypes.MempoolTicketDetails, err error) {
	r, err := s.client.Do("GET", "api/mempool/sstx/details", "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &ticketDetails)
	if err != nil {
		return
	}
	return
}

func (s *Service) GetNHighestTicketDetails(nHighest int) (ticketDetails *apiTypes.MempoolTicketDetails, err error) {
	r, err := s.client.Do("GET", "api/mempool/sstx/details/"+strconv.Itoa(nHighest), "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &ticketDetails)
	if err != nil {
		return
	}
	return
}
