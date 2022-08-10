package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/decred/dcrd/chaincfg/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	apiTypes "github.com/decred/dcrdata/v7/api/types"
)

type Backend int
type Service struct {
	client      *Client
	chainParams *chaincfg.Params
	backendUrl  map[string]map[Backend]string
	urlMu       sync.Mutex
}

const (
	Bittrex Backend = iota
	Binance
	BlockBook
	DcrData
	KuCoin
)

const (
	testnetAddressIndetifier = "T"
	mainnetAddressIdentifier = "D"
	mainnetXpubIdentifier    = "d"
	testnetXpubIdentifier    = "t"
)

var (
	mainnetUrl = map[Backend]string{
		Bittrex:   "https://api.bittrex.com/v3",
		Binance:   "https://api.binance.com",
		BlockBook: "https://blockbook.decred.org:9161/",
		DcrData:   "https://mainnet.dcrdata.org/",
		KuCoin:    "https://api.kucoin.com",
	}

	testnetUrl = map[Backend]string{
		Binance:   "https://testnet.binance.vision",
		BlockBook: "https://blockbook.decred.org:19161/",
		DcrData:   "https://testnet.dcrdata.org/",
		KuCoin:    "https://openapi-sandbox.kucoin.com",
	}

	supportedBackends = []string{"Bittrex", "Binance", "BlockBook", "DcrData", "KuCoin"}
)

func NewService(chainParams *chaincfg.Params) *Service {
	conf := &ClientConf{
		Debug: true,
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
		backendUrl: map[string]map[Backend]string{
			chaincfg.MainNetParams().Name:  mainnetUrl,
			chaincfg.TestNet3Params().Name: testnetUrl,
		},
	}
}

// GetBestBlock returns the best block height as int32.
func (s *Service) GetBestBlock() int32 {
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackendMainnet(DcrData)
	// Use mainnet base url for exchange rate endpoint
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
	s.setBackendMainnet(DcrData)
	// Use mainnet base url for exchanges endpoint
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

func (s *Service) GetTicketFeeRateSummary() (ticketInfo *apiTypes.MempoolTicketFeeInfo, err error) {
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackend(DcrData)
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
	s.setBackend(BlockBook)
	if address == "" {
		err = errors.New("address can't be empty")
		return
	}

	// on testnet, address prefix - first byte - should match testnet identifier
	if s.chainParams.Name == chaincfg.TestNet3Params().Name && address[:1] != testnetAddressIndetifier {
		return nil, errors.New("Net is testnet3 and xpub is not in testnet format")
	}

	// on mainnet, address prefix - first byte - should match mainnet identifier
	if s.chainParams.Name == chaincfg.MainNetParams().Name && address[:1] != mainnetAddressIdentifier {
		return nil, errors.New("Net is mainnet and xpub is not in mainnet format")
	}

	r, err := s.client.Do("GET", "api/v2/address/"+address, "")
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
	s.setBackend(BlockBook)
	if xPub == "" {
		return nil, errors.New("empty xpub string")
	}

	// on testnet Xpub prefix - first byte - should match testnet identifier
	if s.chainParams.Name == chaincfg.TestNet3Params().Name && xPub[:1] != testnetXpubIdentifier {
		return nil, errors.New("Net is testnet3 and xpub is not in testnet format")
	}

	// on mainnet xpup prefix - first byte - should match mainnet identifier
	if s.chainParams.Name == chaincfg.MainNetParams().Name && xPub[:1] != mainnetXpubIdentifier {
		return nil, errors.New("Net is mainnet and xpub is not in mainnet format")
	}

	r, err := s.client.Do("GET", "api/v2/xpub/"+xPub, "")
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
func (s *Service) GetTicker(exchange Backend, market string) (ticker *Ticker, err error) {
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
	s.setBackendMainnet(Binance)
	r, err := s.client.Do("GET", "/api/v3/ticker/24hr?symbol="+strings.ToUpper(market), "")
	if err != nil {
		return ticker, err
	}

	var tempTicker BinanceTicker
	err = json.Unmarshal(r, &tempTicker)
	if err != nil {
		return ticker, err
	}

	ticker = &Ticker{
		Exchange:       supportedBackends[Binance],
		Symbol:         tempTicker.Symbol,
		AskPrice:       tempTicker.AskPrice,
		BidPrice:       tempTicker.BidPrice,
		LastTradePrice: tempTicker.LastPrice,
	}

	return
}

func (s *Service) getBittrexTicker(market string) (ticker *Ticker, err error) {
	s.setBackendMainnet(Bittrex)
	r, err := s.client.Do("GET", "/markets/"+strings.ToUpper(market)+"/ticker", "")
	if err != nil {
		return ticker, err
	}

	var bTicker BittrexTicker
	err = json.Unmarshal(r, &bTicker)
	if err != nil {
		return ticker, err
	}
	ticker = &Ticker{
		Exchange:       supportedBackends[Bittrex],
		Symbol:         bTicker.Symbol,
		AskPrice:       bTicker.Ask,
		BidPrice:       bTicker.Bid,
		LastTradePrice: bTicker.LastTradeRate,
	}

	return
}

func (s *Service) getKucoinTicker(market string) (ticker *Ticker, err error) {
	s.setBackendMainnet(KuCoin)
	r, err := s.client.Do("GET", "/api/v1/market/orderbook/level1?symbol="+strings.ToUpper(market), "")
	if err != nil {
		return ticker, err
	}

	var kTicker KuCoinTicker
	err = json.Unmarshal(r, &kTicker)
	if err != nil {
		return ticker, err
	}
	ticker = &Ticker{
		Exchange:       supportedBackends[KuCoin],
		Symbol:         strings.ToUpper(market),
		AskPrice:       kTicker.Data.BestAsk,
		BidPrice:       kTicker.Data.BestBid,
		LastTradePrice: kTicker.Data.Price,
	}

	return
}

func (s *Service) setBackend(backend Backend) {
	s.urlMu.Lock()
	defer s.urlMu.Unlock()

	if url, ok := s.backendUrl[s.chainParams.Name][backend]; ok {
		s.client.BaseUrl = url
	}
}

func (s *Service) setBackendMainnet(backend Backend) {
	s.urlMu.Lock()
	defer s.urlMu.Unlock()

	if url, ok := s.backendUrl[chaincfg.MainNetParams().Name][backend]; ok {
		s.client.BaseUrl = url
	}
}
