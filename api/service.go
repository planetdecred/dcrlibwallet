package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/decred/dcrd/chaincfg/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	apiTypes "github.com/decred/dcrdata/v7/api/types"
)

type Service struct {
	client      *Client
	chainParams *chaincfg.Params
}

type Exchange int

const (
	Bittrex Exchange = iota
	Binance
	KuCoin
)

const (
	mainnetBaseUrl        = "https://mainnet.dcrdata.org/"
	testnetBaseUrl        = "https://testnet.dcrdata.org/"
	blockbookMainnet      = "https://blockbook.decred.org:9161/"
	blockbookTestnet      = "https://blockbook.decred.org:19161/"
	binanceBaseUrl        = "https://api.binance.com"
	binanceTestnetBaseUrl = "https://testnet.binance.vision"
	bittrexBaseUrl        = "https://api.bittrex.com/v3"
	kucoinBaseUrl         = "https://api.kucoin.com"
	KuCoinTestnetBaseUrl  = "https://openapi-sandbox.kucoin.com"
)

var (
	supportedExchanges = []string{"Bittrex", "Binance", "KuCoin"}
)

func NewService(chainParams *chaincfg.Params) *Service {
	conf := &ClientConf{
		Debug: true,
	}
	if chainParams.Name == chaincfg.TestNet3Params().Name {
		conf.BaseUrl = testnetBaseUrl
	} else {
		conf.BaseUrl = mainnetBaseUrl
	}

	client := NewClient(conf)
	client.RequestFilter = func(info RequestInfo) (req *http.Request, err error) {
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
		log.Error(err)
		return -1
	}

	h, err := strconv.ParseInt(string(r), 10, 32)
	if err != nil {
		log.Error(err)
		return -1
	}

	return int32(h)
}

// GetBestBlockTimeStamp returns best block time, as unix timestamp.
func (s *Service) GetBestBlockTimeStamp() int64 {
	r, err := s.client.Do("GET", "api/block/best?txtotals=false", "")
	if err != nil {
		log.Error(err)
		return -1
	}
	var blockDataBasic *BlockDataBasic
	err = json.Unmarshal(r, &blockDataBasic)
	if err != nil {
		log.Error(err)
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
func (s *Service) GetAgendaDetails(agendaId string) (agendaDetails *AgendaAPIResponse, err error) {
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
	// Use mainnet base url for exchange rates
	r, err := s.client.Do("GET", mainnetBaseUrl+"api/exchangerate", "")
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
	// Use mainnet base url for exchanges
	r, err := s.client.Do("GET", mainnetBaseUrl+"api/exchanges", "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &state)
	if err != nil {
		return
	}
	return
}

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

// GetAddress returns the balances and transactions of an address.
// The returned transactions are sorted by block height, newest blocks first.
func (s *Service) GetAddress(address string) (addressState *AddressState, err error) {
	blockBookBaseUrl := blockbookMainnet
	if s.chainParams.Name == chaincfg.TestNet3Params().Name {
		blockBookBaseUrl = blockbookTestnet
	}

	r, err := s.client.Do("GET", blockBookBaseUrl+"api/v2/address/"+address, "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &addressState)
	if err != nil {
		return
	}
	return
}

// GetXpub Returns balances and transactions of an xpub.
func (s *Service) GetXpub(xPub string) (xPubBalAndTxs *XpubBalAndTxs, err error) {
	blockBookBaseUrl := blockbookMainnet
	if s.chainParams.Name == chaincfg.TestNet3Params().Name {
		blockBookBaseUrl = blockbookTestnet
	}

	r, err := s.client.Do("GET", blockBookBaseUrl+"api/v2/xpub/"+xPub, "")
	if err != nil {
		return
	}

	err = json.Unmarshal(r, &xPubBalAndTxs)
	if err != nil {
		return
	}
	return
}

// GetTicker returns market ticker data for the supported exchanges.
// Current supported exchanges: bittrex, binance and kucoin.
func (s *Service) GetTicker(exchange Exchange, market string) (ticker *Ticker, err error) {
	switch exchange {
	case Binance:
		symbArr := strings.Split(market, "-")
		if len(symbArr) != 2 {
			return ticker, errors.New("Invalid symbol format")
		}
		symb := strings.Join(symbArr[:], "")
		return s.getBinanceTicker(symb)
	case Bittrex:
		return s.getBittrexTicker(market)
	case KuCoin:
		return s.getKucoinTicker(market)
	}

	return nil, errors.New("Unknown exchange")
}

func (s *Service) getBinanceTicker(market string) (ticker *Ticker, err error) {
	r, err := s.client.Do("GET", binanceBaseUrl+"/api/v3/ticker/24hr?symbol="+strings.ToUpper(market), "")
	if err != nil {
		return ticker, err
	}

	var tempTicker BinanceTicker
	err = json.Unmarshal(r, &tempTicker)
	if err != nil {
		return ticker, err
	}

	ticker = &Ticker{
		Exchange:       supportedExchanges[Binance],
		Symbol:         tempTicker.Symbol,
		AskPrice:       tempTicker.AskPrice,
		BidPrice:       tempTicker.BidPrice,
		LastTradePrice: tempTicker.LastPrice,
	}

	return
}

func (s *Service) getBittrexTicker(market string) (ticker *Ticker, err error) {
	r, err := s.client.Do("GET", bittrexBaseUrl+"/markets/"+strings.ToUpper(market)+"/ticker", "")
	if err != nil {
		return ticker, err
	}

	var bTicker BittrexTicker
	err = json.Unmarshal(r, &bTicker)
	if err != nil {
		return ticker, err
	}
	ticker = &Ticker{
		Exchange:       supportedExchanges[Bittrex],
		Symbol:         bTicker.Symbol,
		AskPrice:       bTicker.Ask,
		BidPrice:       bTicker.Bid,
		LastTradePrice: bTicker.LastTradeRate,
	}

	return
}

func (s *Service) getKucoinTicker(market string) (ticker *Ticker, err error) {
	r, err := s.client.Do("GET", kucoinBaseUrl+"/api/v1/market/orderbook/level1?symbol="+strings.ToUpper(market), "")
	if err != nil {
		return ticker, err
	}

	var kTicker KuCoinTicker
	err = json.Unmarshal(r, &kTicker)
	if err != nil {
		return ticker, err
	}
	ticker = &Ticker{
		Exchange:       supportedExchanges[KuCoin],
		Symbol:         strings.ToUpper(market),
		AskPrice:       kTicker.Data.BestAsk,
		BidPrice:       kTicker.Data.BestBid,
		LastTradePrice: kTicker.Data.Price,
	}

	return
}
