package politeia

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
)

const (
	endpoint            = "proposals.decred.org"
	endpointPath        = "/api/v1"
	versionPath         = "/version"
	policyPath          = "/policy"
	vettedProposalsPath = "/proposals/vetted"
	proposalDetailsPath = "/proposals/%s"
	voteStatusPath      = "/proposals/%s/votestatus"
	votesStatusPath     = "/proposals/votestatus"
)

type Politeia struct {
	csrfToken    string
	cookie       *http.Cookie
	serverPolicy *ServerPolicy
}

func New() Politeia {
	return Politeia{}
}

func (p *Politeia) prepareRequest(path, method string, queryStrings map[string]string, body []byte) (*http.Request, error) {
	req := &http.Request{
		Method: method,
		URL:    &url.URL{Scheme: "https", Host: endpoint, Path: endpointPath + path},
	}

	if body != nil {
		req.Body = ioutil.NopCloser(bytes.NewBuffer(body))
	}

	if queryStrings != nil {
		queryString := req.URL.Query()
		for i, v := range queryStrings {
			queryString.Set(i, v)
		}
		req.URL.RawQuery = queryString.Encode()
	}

	if method == "POST" {
		if p.csrfToken == "" {
			if err := p.getCSRFToken(); err != nil {
				return nil, err
			}
		}
		req.Header.Set("X-CSRF-TOKEN", p.csrfToken)
		req.AddCookie(p.cookie)
	}

	return req, nil
}

func (p *Politeia) getCSRFToken() error {
	req := &http.Request{
		Method: "GET",
		URL:    &url.URL{Scheme: "https", Host: endpoint, Path: endpointPath + versionPath},
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error fetching csrf token")
	}

	p.csrfToken = res.Header.Get("X-CSRF-TOKEN")

	for _, v := range res.Cookies() {
		if v.Name == "_gorilla_csrf" {
			p.cookie = v
			break
		}
	}

	return nil
}

func (p *Politeia) makeRequest(path, method string, queryStrings map[string]string, body []byte, dest interface{}) error {
	req, err := p.prepareRequest(path, method, queryStrings, body)
	if err != nil {
		return err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	return p.handleResponse(res, dest)
}

func (p *Politeia) handleResponse(res *http.Response, dest interface{}) error {
	switch res.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(res.Body).Decode(dest)
	case http.StatusNotFound:
		return errors.New("resource not found")
	case http.StatusInternalServerError:
		return errors.New("internal server error")
	case http.StatusBadRequest:
		var errResp Err
		if err := p.marshalResponse(res, &errResp); err != nil {
			return err
		}
		return fmt.Errorf("bad request: %s", ErrorStatus[errResp.Code])
	}

	return errors.New("an unknown error occurred")
}

func (p *Politeia) marshalResponse(res *http.Response, dest interface{}) error {
	defer res.Body.Close()

	err := json.NewDecoder(res.Body).Decode(dest)
	if err != nil {
		return fmt.Errorf("error decoding response body: %s", err.Error())
	}

	return nil
}

func (p *Politeia) getServerPolicy() (*ServerPolicy, error) {
	var serverPolicy ServerPolicy

	err := p.makeRequest(policyPath, "GET", nil, nil, &serverPolicy)
	if err != nil {
		return nil, fmt.Errorf("error fetching politeia policy: %v", err)
	}
	return &serverPolicy, nil
}
