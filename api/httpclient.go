package api

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

const (
	// Default http client timeout in secs.
	defaultHttpClientTimeout = 10
)

//Client is the base for http calls
type (
	Client struct {
		httpClient *http.Client
		Debug      bool
		BaseUrl    string
		ReqFilter  RequestFilter
	}
	RequestFilter func(info RequestInfo) (req *http.Request, err error)

	// ClientConf Models http client configurations
	ClientConf struct {
		Debug   bool
		BaseUrl string
	}

	//RequestInfo models the http request data.
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
	return &Client{
		httpClient: &http.Client{},
		Debug:      conf.Debug,
		BaseUrl:    conf.BaseUrl,
		ReqFilter:  nil,
	}
}

// doTimeoutRequest sends a HTTP request with a timeout
func (c *Client) doTimeoutRequest(timer *time.Timer, req *http.Request) (*http.Response, error) {
	// Send the request in the background so we can check the timeout
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		if c.Debug {
			c.dumpRequest(req)
		}
		resp, err := c.httpClient.Do(req)
		if c.Debug {
			c.dumpResponse(resp)
		}
		done <- result{resp, err}
	}()
	// Wait for the read or the timeout
	select {
	case r := <-done:
		return r.resp, r.err
	case <-timer.C:
		errTimeout := errors.New("timeout reading from API")
		return nil, errTimeout
	}
}

//Do prepare and process HTTP request to API
func (c *Client) Do(method, resource string, payload interface{}) (response []byte, err error) {
	var connectTimer *time.Timer
	// connect timer should be configureable
	connectTimer = time.NewTimer(defaultHttpClientTimeout * time.Second)
	var rawurl string
	if strings.HasPrefix(resource, "http") {
		rawurl = resource
	} else {
		rawurl = fmt.Sprintf("%s%s", c.BaseUrl, resource)
	}
	var req *http.Request

	reqInfo := RequestInfo{
		client:  c,
		Method:  method,
		Payload: payload,
		Url:     rawurl,
	}

	if c.ReqFilter == nil {
		err = errors.New("Request Filter was not set")
		return
	}

	req, err = c.ReqFilter(reqInfo)

	if err != nil {
		return nil, err
	}
	if req == nil {
		err = errors.New("error: nil request")
		return nil, err
	}

	resp, err := c.doTimeoutRequest(connectTimer, req)
	if err != nil {
		return
	}

	defer resp.Body.Close()
	response, err = ioutil.ReadAll(resp.Body)

	//test
	if c.Debug {
		fmt.Printf("\n|*** URL %s RESPONSE ***|\n", req.URL)
		if err != nil {
			fmt.Printf("%s err: %s", response, err.Error())
		} else {
			fmt.Printf("%s", response)
		}
		fmt.Printf("\n|*** END RESPONSE ***|\n")
	}

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
		log.Print("dumpReq ok: <nil>")
		return
	}
	dump, err := httputil.DumpRequest(r, true)
	if err != nil {
		log.Print("dumpReq err:", err)
	} else {
		log.Print("dumpReq ok:", string(dump))
	}
}

func (c *Client) dumpResponse(r *http.Response) {
	if r == nil {
		log.Print("dumpResponse ok: <nil>")
		return
	}
	dump, err := httputil.DumpResponse(r, true)
	if err != nil {
		log.Print("dumpResponse err:", err)
	} else {
		log.Print("dumpResponse ok:", string(dump))
	}
}
