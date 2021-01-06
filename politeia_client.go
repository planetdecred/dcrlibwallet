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

type politeiaClient struct {
	httpClient *http.Client

	policy             *www.PolicyReply
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
		var errResp www.ErrorReply
		if err := json.Unmarshal(responseBody, &errResp); err != nil {
			return err
		}
		return fmt.Errorf("unauthorized: %d", errResp.ErrorCode)
	case http.StatusBadRequest:
		var errResp www.ErrorReply
		if err := json.Unmarshal(responseBody, &errResp); err != nil {
			return err
		}
		return fmt.Errorf("bad request: %d", errResp.ErrorCode)
	}

	return errors.New("unknown error")
}

func (c *politeiaClient) version() (*www.VersionReply, error) {
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

	var versionReply www.VersionReply
	err = json.Unmarshal(responseBody, &versionReply)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling version response: %s", err.Error())
	}

	newCsrfToken := r.Header.Get(www.CsrfToken)
	if newCsrfToken != "" {
		c.csrfToken = newCsrfToken
	}
	c.csrfTokenExpiresAt = time.Now().Add(time.Hour * 23)

	return &versionReply, nil
}

func (c *politeiaClient) serverPolicy() (www.PolicyReply, error) {
	var policyReply www.PolicyReply
	err := c.makeRequest(http.MethodGet, policyPath, nil, &policyReply)
	return policyReply, err
}

func (c *politeiaClient) batchProposals(tokens []string) ([]Proposal, error) {
	b, err := json.Marshal(&www.BatchProposals{Tokens: tokens})
	if err != nil {
		return nil, err
	}

	var batchProposalsReply www.BatchProposalsReply

	err = c.makeRequest(http.MethodPost, batchProposalsPath, b, &batchProposalsReply)
	if err != nil {
		return nil, err
	}

	proposals := make([]Proposal, len(batchProposalsReply.Proposals))
	for i, proposalRecord := range batchProposalsReply.Proposals {
		proposal := Proposal{
			Token:       proposalRecord.CensorshipRecord.Token,
			Name:        proposalRecord.Name,
			State:       int32(proposalRecord.State),
			Status:      int32(proposalRecord.Status),
			Timestamp:   proposalRecord.Timestamp,
			UserID:      proposalRecord.UserId,
			Username:    proposalRecord.Username,
			NumComments: int32(proposalRecord.NumComments),
			PublishedAt: proposalRecord.PublishedAt,
		}

		for _, file := range proposalRecord.Files {
			if file.Name == "index.md" {
				proposal.IndexFile = file.Payload
				break
			}
		}

		proposals[i] = proposal
	}

	return proposals, nil
}

func (c *politeiaClient) tokenInventory() (*www.TokenInventoryReply, error) {
	var tokenInventoryReply www.TokenInventoryReply

	err := c.makeRequest(http.MethodGet, tokenInventoryPath, nil, &tokenInventoryReply)
	if err != nil {
		return nil, err
	}

	return &tokenInventoryReply, nil
}

func (c *politeiaClient) batchVoteSummary(tokens []string) (map[string]www.VoteSummary, error) {
	b, err := json.Marshal(&www.BatchVoteSummary{Tokens: tokens})
	if err != nil {
		return nil, err
	}

	var batchVoteSummaryReply www.BatchVoteSummaryReply

	err = c.makeRequest(http.MethodPost, batchVoteSummaryPath, b, &batchVoteSummaryReply)
	if err != nil {
		return nil, err
	}

	return batchVoteSummaryReply.Summaries, err
}
