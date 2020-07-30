package dcrlibwallet

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	www "github.com/decred/politeia/politeiawww/api/www/v1"
	"github.com/decred/politeia/util"
)

type proposalServerVersion struct {
	Version int32 `json:"version"`
}

type proposalServerPolicy struct {
	ProposalListPageSize int `json:"proposallistpagesize"`
}

type proposalFile struct {
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Digest  string `json:"digest"`
	Payload string `json:"payload"`
}

type proposalMetaData struct {
	Name   string `json:"name"`
	LinkTo string `json:"linkto"`
	LinkBy int64  `json:"linkby"`
}

type proposalCensorshipRecord struct {
	Token     string `json:"token" storm:"index"`
	Merkle    string `json:"merkle"`
	Signature string `json:"signature"`
}

type proposalVoteOption struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Bits        int32  `json:"bits"`
}

type ProposalVoteOptionResult struct {
	Option        proposalVoteOption `json:"option"`
	VotesReceived int64              `json:"votesreceived"`
}

type proposalVoteSummary struct {
	Token            string                     `json:"token"`
	Status           int32                      `json:"status"`
	Approved         bool                       `json:"approved,omitempty"`
	EligibleTickets  int32                      `json:"eligibletickets"`
	Duration         int64                      `json:"duration,omitempty"`
	EndHeight        int64                      `json:"endheight,omitempty"`
	QuorumPercentage int32                      `json:"quorumpercentage,omitempty"`
	PassPercentage   int32                      `json:"passpercentage,omitempty"`
	YesCount         int32                      `json:"yes-count"`
	NoCount          int32                      `json:"no-count"`
	OptionsResult    []ProposalVoteOptionResult `json:"results,omitempty"`
}

type proposalTokenInventory struct {
	Pre       []string `json:"pre"`
	Active    []string `json:"active"`
	Approved  []string `json:"approved"`
	Rejected  []string `json:"rejected"`
	Abandoned []string `json:"abandoned"`
}

type proposalTokens struct {
	Tokens []string `json:"tokens"`
}

type proposalResponseError struct {
	Code    uint16 `json:"code"`
	Message string `json:"message"`
}

type proposalResponse struct {
	Result interface{}            `json:"result"`
	Error  *proposalResponseError `json:"error"`
}

type politeiaError struct {
	Code    uint16   `json:"errorcode"`
	Context []string `json:"errorcontext"`
}

type politeiaClient struct {
	httpClient *http.Client

	policy             *proposalServerPolicy
	csrfToken          string
	cookies            []*http.Cookie
	csrfTokenExpiresAt time.Time
}

const (
	host    = "https://proposals.decred.org"
	apiPath = "/api/v1"

	versionPath          = "/version"
	policyPath           = "/policy"
	votesStatusPath      = "/proposals/votestatus"
	tokenInventoryPath   = "/proposals/tokeninventory"
	batchProposalsPath   = "/proposals/batch"
	batchVoteSummaryPath = "/proposals/batchvotesummary"

	ErrNotFound uint16 = iota + 1
	ErrUnknownError
)

var (
	ErrorStatus = map[uint16]string{
		ErrNotFound:     "no record found",
		ErrUnknownError: "an unknown error occurred",
	}
)

func newPoliteiaClient() *politeiaClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	httpClient := &http.Client{
		Transport: tr,
		Timeout:   time.Second * 60,
	}

	return &politeiaClient{
		httpClient: httpClient,
	}
}

func (c *politeiaClient) getRequestBody(method string, body interface{}) ([]byte, error) {
	if body == nil {
		return nil, nil
	}

	if method == http.MethodPost {
		if requestBody, ok := body.([]byte); ok {
			return requestBody, nil
		}
	} else if method == http.MethodGet {
		if requestBody, ok := body.(map[string]string); ok {
			params := url.Values{}
			for key, val := range requestBody {
				params.Add(key, val)
			}
			return []byte(params.Encode()), nil
		}
	}

	return nil, errors.New("invalid request body")
}

func (c *politeiaClient) makeRequest(method, path string, body interface{}, dest interface{}) error {
	var err error
	var requestBody []byte

	if c.csrfToken == "" || time.Now().Unix() >= c.csrfTokenExpiresAt.Unix() {
		_, err := c.version()
		if err != nil {
			return err
		}
	}

	route := host + apiPath + path
	if body != nil {
		requestBody, err = c.getRequestBody(method, body)
		if err != nil {
			return err
		}
	}

	if method == http.MethodGet && requestBody != nil {
		route += string(requestBody)
	}

	// Create http request
	req, err := http.NewRequest(method, route, nil)
	if err != nil {
		return fmt.Errorf("error creating http request: %s", err.Error())
	}
	if method == http.MethodPost && requestBody != nil {
		req.Body = ioutil.NopCloser(bytes.NewReader(requestBody))
	}
	req.Header.Add(www.CsrfToken, c.csrfToken)

	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}

	// Send request
	r, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		r.Body.Close()
	}()

	responseBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}

	if r.StatusCode != http.StatusOK {
		return c.handleError(r.StatusCode, responseBody)
	}

	err = json.Unmarshal(responseBody, dest)
	if err != nil {
		return fmt.Errorf("error unmarshaling response: %s", err.Error())
	}

	return nil
}

func (c *politeiaClient) handleError(statusCode int, responseBody []byte) error {
	switch statusCode {
	case http.StatusNotFound:
		return errors.New("resource not found")
	case http.StatusInternalServerError:
		return errors.New("internal server error")
	case http.StatusForbidden:
		return errors.New(string(responseBody))
	case http.StatusUnauthorized:
		var errResp politeiaError
		if err := json.Unmarshal(responseBody, &errResp); err != nil {
			return err
		}
		return fmt.Errorf("unauthorized: %s", ErrorStatus[errResp.Code])
	case http.StatusBadRequest:
		var errResp politeiaError
		if err := json.Unmarshal(responseBody, &errResp); err != nil {
			return err
		}
		return fmt.Errorf("bad request: %s", ErrorStatus[errResp.Code])
	}

	return errors.New("unknown error")
}

func (c *politeiaClient) version() (*proposalServerVersion, error) {
	route := host + apiPath + versionPath
	req, err := http.NewRequest(http.MethodGet, route, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating version request: %s", err.Error())
	}
	req.Header.Add(www.CsrfToken, c.csrfToken)

	// Send request
	r, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching politeia server version: %s", err.Error())
	}
	defer func() {
		r.Body.Close()
	}()

	c.cookies = r.Cookies()

	responseBody := util.ConvertBodyToByteArray(r.Body, false)
	if r.StatusCode != http.StatusOK {
		return nil, c.handleError(r.StatusCode, responseBody)
	}

	var versionResponse proposalServerVersion
	err = json.Unmarshal(responseBody, &versionResponse)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling version response: %s", err.Error())
	}

	newCsrfToken := r.Header.Get(www.CsrfToken)
	if newCsrfToken != "" {
		c.csrfToken = newCsrfToken
	}
	c.csrfTokenExpiresAt = time.Now().Add(time.Hour * 23)

	return &versionResponse, nil
}

func (c *politeiaClient) serverPolicy() (proposalServerPolicy, error) {
	var serverPolicyResponse proposalServerPolicy
	err := c.makeRequest(http.MethodGet, policyPath, nil, &serverPolicyResponse)

	return serverPolicyResponse, err
}

func (c *politeiaClient) batchProposals(censorshipTokens *proposalTokens) ([]Proposal, error) {
	b, err := json.Marshal(censorshipTokens)
	if err != nil {
		return nil, err
	}

	var result struct {
		Proposals []Proposal `json:"proposals"`
	}

	err = c.makeRequest(http.MethodPost, batchProposalsPath, b, &result)
	if err != nil {
		return nil, err
	}

	return result.Proposals, err
}

func (c *politeiaClient) tokenInventory() (*proposalTokenInventory, error) {
	var tokenInventory proposalTokenInventory

	err := c.makeRequest(http.MethodGet, tokenInventoryPath, nil, &tokenInventory)
	if err != nil {
		return nil, err
	}

	return &tokenInventory, nil
}

func (c *politeiaClient) batchVoteSummary(censorshipTokens *proposalTokens) (map[string]proposalVoteSummary, error) {
	if censorshipTokens == nil {
		return nil, errors.New("censorship token cannot be empty")
	}

	b, err := json.Marshal(censorshipTokens)
	if err != nil {
		return nil, err
	}

	var result struct {
		Summaries map[string]proposalVoteSummary `json:"summaries"`
	}

	err = c.makeRequest(http.MethodPost, batchVoteSummaryPath, b, &result)
	if err != nil {
		return nil, err
	}

	return result.Summaries, err
}
