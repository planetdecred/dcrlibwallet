package api

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

const (
	// Default http client timeout in secs.
	defaultHttpClientTimeout = 10 * time.Second
)

type (
	// Client is the base for http/https calls
	Client struct {
		httpClient    *http.Client
		Debug         bool
		BaseUrl       string
		RequestFilter func(info RequestInfo) (req *http.Request, err error)
	}

	// ClientConf Models http client configurations
	ClientConf struct {
		Debug   bool
		BaseUrl string
	}

	// RequestInfo models the http request data.
	RequestInfo struct {
		client  *Client
		request *http.Request
		Payload interface{}
		Method  string
		Url     string
	}
)

// NewClient return a new HTTP client
func NewClient(conf *ClientConf) (c *Client) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	client := &http.Client{
		Timeout:   defaultHttpClientTimeout,
		Transport: t,
	}

	return &Client{
		httpClient:    client,
		Debug:         conf.Debug,
		BaseUrl:       conf.BaseUrl,
		RequestFilter: nil,
	}
}

// Do prepare and process HTTP request to API
func (c *Client) Do(method, resource string, payload interface{}) (response []byte, err error) {
	var rawurl = fmt.Sprintf("%s%s", c.BaseUrl, resource)
	if strings.HasPrefix(resource, "http") {
		rawurl = resource
	}

	var req *http.Request
	reqInfo := RequestInfo{
		client:  c,
		Method:  method,
		Payload: payload,
		Url:     rawurl,
	}

	if c.RequestFilter == nil {
		return nil, errors.New("Request Filter was not set")
	}

	req, err = c.RequestFilter(reqInfo)
	if err != nil {
		return nil, err
	}

	if req == nil {
		return nil, errors.New("error: nil request")
	}

	c.dumpRequest(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return response, err
	}
	c.dumpResponse(resp)

	defer resp.Body.Close()
	response, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return response, err
	}

	if resp.StatusCode != 200 {
		var errStr string
		responseStr := string(response)

		res := "'" + responseStr + "'"
		errStr = "\nerror:" + resp.Status + ":" + res
		err = errors.New(errStr)
	}
	return response, err
}

func (c *Client) dumpRequest(r *http.Request) {
	if r == nil {
		log.Debug("dumpReq ok: <nil>")
		return
	}
	dump, err := httputil.DumpRequest(r, true)
	if err != nil {
		log.Debug("dumpReq err: %v", err)
	} else {
		log.Debug("dumpReq ok: %v", string(dump))
	}
}

func (c *Client) dumpResponse(r *http.Response) {
	if r == nil {
		log.Debug("dumpResponse ok: <nil>")
		return
	}
	dump, err := httputil.DumpResponse(r, true)
	if err != nil {
		log.Debug("dumpResponse err: %v", err)
	} else {
		log.Debug("dumpResponse ok: %v", string(dump))
	}
}
