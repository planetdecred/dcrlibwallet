package api

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/decred/dcrd/chaincfg/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	apiTypes "github.com/decred/dcrdata/v7/api/types"
)

type (
	Backend string
	Service struct {
		client      *Client
		chainParams *chaincfg.Params
	}
)

const (
	Bittrex                  Backend = "bittrex"
	Binance                  Backend = "binance"
	BlockBook                Backend = "blockbook"
	DcrData                  Backend = "dcrdata"
	KuCoin                   Backend = "kucoin"
	testnetAddressIndetifier         = "T"
	mainnetAddressIdentifier         = "D"
	mainnetXpubIdentifier            = "d"
	testnetXpubIdentifier            = "t"
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

	backendUrl = map[string]map[Backend]string{
		chaincfg.MainNetParams().Name:  mainnetUrl,
		chaincfg.TestNet3Params().Name: testnetUrl,
	}
)

func NewService(chainParams *chaincfg.Params) *Service {
	client := NewClient()
	client.RequestFilter = func(reqConfig *ReqConfig) (req *http.Request, err error) {
		req, err = http.NewRequest(reqConfig.method, reqConfig.url, bytes.NewBuffer(reqConfig.payload))
		if err != nil {
			log.Error(err)
			return
		}
		if reqConfig.method == http.MethodPost || reqConfig.method == http.MethodPut {
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
	reqConf := &ReqConfig{
		method:  http.MethodGet,
		url:     "api/block/best/height",
		retByte: true,
	}

	var resp []byte
	err := s.client.Do(DcrData, s.chainParams.Name, reqConf, &resp)
	if err != nil {
		log.Error(err)
		return -1
	}

	h, err := strconv.ParseInt(string(resp), 10, 32)
	if err != nil {
		log.Error(err)
		return -1
	}

	return int32(h)
}

// GetBestBlockTimeStamp returns best block time, as unix timestamp.
func (s *Service) GetBestBlockTimeStamp() int64 {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/block/best?txtotals=false",
	}

	resp := &BlockDataBasic{}
	err := s.client.Do(DcrData, s.chainParams.Name, reqConf, resp)
	if err != nil {
		log.Error(err)
		return -1
	}
	return resp.Time.UNIX()
}

// GetCurrentAgendaStatus returns the current agenda and its status.
func (s *Service) GetCurrentAgendaStatus() (agenda *chainjson.GetVoteInfoResult, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/stake/vote/info",
	}
	aStatus := &chainjson.GetVoteInfoResult{}
	return aStatus, s.client.Do(DcrData, s.chainParams.Name, reqConf, aStatus)
}

// GetAgendas returns all agendas high level details
func (s *Service) GetAgendas() (agendas *[]apiTypes.AgendasInfo, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/agendas",
	}
	aList := &[]apiTypes.AgendasInfo{}
	return aList, s.client.Do(DcrData, s.chainParams.Name, reqConf, aList)
}

// GetAgendaDetails returns the details for agenda with agendaId
func (s *Service) GetAgendaDetails(agendaId string) (agendaDetails *AgendaAPIResponse, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/agenda/" + agendaId,
	}
	aDetails := &AgendaAPIResponse{}
	return aDetails, s.client.Do(DcrData, s.chainParams.Name, reqConf, aDetails)
}

// GetTreasuryBalance returns the current treasury balance as int64.
func (s *Service) GetTreasuryBalance() (bal int64, err error) {
	treasury, err := s.GetTreasuryDetails()
	if err != nil {
		return bal, err
	}
	return treasury.Balance, err
}

// GetTreasuryDetails the current tresury balance, spent amount, added amount, and tx count for the
// treasury.
func (s *Service) GetTreasuryDetails() (treasuryDetails *TreasuryDetails, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/treasury/balance",
	}
	tDetails := &TreasuryDetails{}
	return tDetails, s.client.Do(DcrData, s.chainParams.Name, reqConf, tDetails)
}

// GetExchangeRate fetches exchange rate data summary
func (s *Service) GetExchangeRate() (rates *ExchangeRates, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/exchangerate",
	}
	exRates := &ExchangeRates{}
	// Use mainnet base url for exchange rate endpoint
	return exRates, s.client.Do(DcrData, chaincfg.MainNetParams().Name, reqConf, exRates)
}

// GetExchanges fetches the current known state of all exchanges
func (s *Service) GetExchanges() (state *ExchangeState, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/exchanges",
	}
	exState := &ExchangeState{}
	// Use mainnet base url for exchanges endpoint
	return exState, s.client.Do(DcrData, chaincfg.MainNetParams().Name, reqConf, exState)
}

func (s *Service) GetTicketFeeRateSummary() (ticketInfo *apiTypes.MempoolTicketFeeInfo, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/mempool/sstx",
	}
	tFeeSum := &apiTypes.MempoolTicketFeeInfo{}
	return tFeeSum, s.client.Do(DcrData, s.chainParams.Name, reqConf, tFeeSum)
}

func (s *Service) GetTicketFeeRate() (ticketFeeRate *apiTypes.MempoolTicketFees, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/mempool/sstx/fees",
	}
	tFeeRate := &apiTypes.MempoolTicketFees{}
	return tFeeRate, s.client.Do(DcrData, s.chainParams.Name, reqConf, tFeeRate)
}

func (s *Service) GetNHighestTicketFeeRate(nHighest int) (ticketFeeRate *apiTypes.MempoolTicketFees, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/mempool/sstx/fees/" + strconv.Itoa(nHighest),
	}
	nTFeeRate := &apiTypes.MempoolTicketFees{}
	return nTFeeRate, s.client.Do(DcrData, s.chainParams.Name, reqConf, nTFeeRate)
}

func (s *Service) GetTicketDetails() (ticketDetails *apiTypes.MempoolTicketDetails, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/mempool/sstx/details",
	}
	tDetails := &apiTypes.MempoolTicketDetails{}
	return tDetails, s.client.Do(DcrData, s.chainParams.Name, reqConf, tDetails)
}

func (s *Service) GetNHighestTicketDetails(nHighest int) (ticketDetails *apiTypes.MempoolTicketDetails, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/mempool/sstx/details/" + strconv.Itoa(nHighest),
	}
	nTDetails := &apiTypes.MempoolTicketDetails{}
	return nTDetails, s.client.Do(DcrData, s.chainParams.Name, reqConf, nTDetails)
}

// GetAddress returns the balances and transactions of an address.
// The returned transactions are sorted by block height, newest blocks first.
func (s *Service) GetAddress(address string) (addressState *AddressState, err error) {
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

	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/v2/address/" + address,
	}
	addrState := &AddressState{}
	return addrState, s.client.Do(BlockBook, s.chainParams.Name, reqConf, addrState)
}

// GetXpub Returns balances and transactions of an xpub.
func (s *Service) GetXpub(xPub string) (xPubBalAndTxs *XpubBalAndTxs, err error) {
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

	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "api/v2/xpub/" + xPub,
	}
	xPubTxs := &XpubBalAndTxs{}
	return xPubTxs, s.client.Do(BlockBook, s.chainParams.Name, reqConf, xPubTxs)
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
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "/api/v3/ticker/24hr?symbol=" + strings.ToUpper(market),
	}
	tempTicker := &BinanceTicker{}
	err = s.client.Do(Binance, chaincfg.MainNetParams().Name, reqConf, tempTicker)
	if err != nil {
		return
	}
	ticker = &Ticker{
		Exchange:       string(Binance),
		Symbol:         tempTicker.Symbol,
		AskPrice:       tempTicker.AskPrice,
		BidPrice:       tempTicker.BidPrice,
		LastTradePrice: tempTicker.LastPrice,
	}

	return
}

func (s *Service) getBittrexTicker(market string) (ticker *Ticker, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "/markets/" + strings.ToUpper(market) + "/ticker",
	}
	bTicker := &BittrexTicker{}
	err = s.client.Do(Bittrex, chaincfg.MainNetParams().Name, reqConf, bTicker)
	if err != nil {
		return
	}
	ticker = &Ticker{
		Exchange:       string(Bittrex),
		Symbol:         bTicker.Symbol,
		AskPrice:       bTicker.Ask,
		BidPrice:       bTicker.Bid,
		LastTradePrice: bTicker.LastTradeRate,
	}

	return
}

func (s *Service) getKucoinTicker(market string) (ticker *Ticker, err error) {
	reqConf := &ReqConfig{
		method: http.MethodGet,
		url:    "/api/v1/market/orderbook/level1?symbol=" + strings.ToUpper(market),
	}
	kTicker := &KuCoinTicker{}
	err = s.client.Do(KuCoin, chaincfg.MainNetParams().Name, reqConf, kTicker)
	if err != nil {
		return
	}

	// Kucoin doesn't send back error code if it doesn't support the supplied market.
	// We should filter those instances using the sequence number.
	// When sequence is 0, no ticker data was returned.
	if kTicker.Data.Sequence == 0 {
		return nil, errors.New("An error occured. Most likely unsupported Kucoin market.")
	}

	ticker = &Ticker{
		Exchange:       string(KuCoin),
		Symbol:         strings.ToUpper(market),
		AskPrice:       kTicker.Data.BestAsk,
		BidPrice:       kTicker.Data.BestBid,
		LastTradePrice: kTicker.Data.Price,
	}

	return
}
