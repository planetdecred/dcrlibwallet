package dcrlibwallet

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"

	"github.com/decred/dcrd/dcrjson"
)

func CallJSONRPC(method string, args string, address string, username string, password string, caCert string) (string, error) {
	arguments := strings.Split(args, ",")
	params := make([]interface{}, 0)
	for _, arg := range arguments {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		params = append(params, strings.TrimSpace(arg))
	}
	// Attempt to create the appropriate command using the arguments
	// provided by the user.
	cmd, err := dcrjson.NewCmd(method, params...)
	if err != nil {
		// Show the error along with its error code when it's a
		// dcrjson.Error as it reallistcally will always be since the
		// NewCmd function is only supposed to return errors of that
		// type.
		if jerr, ok := err.(dcrjson.Error); ok {
			log.Errorf("%s command: %v (code: %s)\n",
				method, err, jerr.Code)
			return "", err
		}
		// The error is not a dcrjson.Error and this really should not
		// happen.  Nevertheless, fallback to just showing the error
		// if it should happen due to a bug in the package.
		log.Errorf("%s command: %v\n", method, err)
		return "", err
	}

	// Marshal the command into a JSON-RPC byte slice in preparation for
	// sending it to the RPC server.
	marshalledJSON, err := dcrjson.MarshalCmd("1.0", 1, cmd)
	if err != nil {
		log.Error(err)
		return "", err
	}

	// Send the JSON-RPC request to the server using the user-specified
	// connection configuration.
	result, err := sendPostRequest(marshalledJSON, address, username, password, caCert)
	if err != nil {
		log.Error(err)
		return "", err
	}

	// Choose how to display the result based on its type.
	strResult := string(result)
	if strings.HasPrefix(strResult, "{") || strings.HasPrefix(strResult, "[") {
		var dst bytes.Buffer
		if err := json.Indent(&dst, result, "", "  "); err != nil {
			log.Errorf("Failed to format result: %v", err)
			return "", err
		}
		fmt.Println(dst.String())
		return dst.String(), nil

	} else if strings.HasPrefix(strResult, `"`) {
		var str string
		if err := json.Unmarshal(result, &str); err != nil {
			log.Errorf("Failed to unmarshal result: %v", err)
			return "", err
		}
		fmt.Println(str)
		return str, nil

	} else if strResult != "null" {
		fmt.Println(strResult)
		return strResult, nil
	}
	return "", nil
}

// sendPostRequest sends the marshalled JSON-RPC command using HTTP-POST mode
// to the server described in the passed config struct.  It also attempts to
// unmarshal the response as a JSON-RPC response and returns either the result
// field or the error field depending on whether or not there is an error.
func sendPostRequest(marshalledJSON []byte, rpcServer string, username string, password string, cert string) ([]byte, error) {
	// Generate a request to the configured RPC server.
	protocol := "https"
	url := protocol + "://" + rpcServer
	bodyReader := bytes.NewReader(marshalledJSON)
	httpRequest, err := http.NewRequest("POST", url, bodyReader)
	if err != nil {
		return nil, err
	}
	httpRequest.Close = true
	httpRequest.Header.Set("Content-Type", "application/json")

	// Configure basic access authorization.
	httpRequest.SetBasicAuth(username, password)

	// Create the new HTTP client that is configured according to the user-
	// specified options and submit the request.
	httpClient, err := newHTTPClient(cert)
	if err != nil {
		return nil, err
	}
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}

	// Read the raw bytes and close the response.
	respBytes, err := ioutil.ReadAll(httpResponse.Body)
	httpResponse.Body.Close()
	if err != nil {
		err = fmt.Errorf("error reading json reply: %v", err)
		return nil, err
	}

	// Handle unsuccessful HTTP responses
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		// Generate a standard error to return if the server body is
		// empty.  This should not happen very often, but it's better
		// than showing nothing in case the target server has a poor
		// implementation.
		if len(respBytes) == 0 {
			return nil, fmt.Errorf("%d %s", httpResponse.StatusCode,
				http.StatusText(httpResponse.StatusCode))
		}
		return nil, fmt.Errorf("%s", respBytes)
	}

	// Print raw json response.
	fmt.Println(string(respBytes))

	// Unmarshal the response.
	var resp dcrjson.Response
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// newHTTPClient returns a new HTTP client that is configured according to the
//  TLS settings in the associated connection configuration.
func newHTTPClient(cert string) (*http.Client, error) {
	var dial func(network, addr string) (net.Conn, error)
	// Configure TLS
	var tlsConfig *tls.Config
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM([]byte(cert)); !ok {
		return nil, fmt.Errorf("invalid certificate file: %v",
			cert)
	}
	tlsConfig = &tls.Config{
		RootCAs:            pool,
		InsecureSkipVerify: false,
	}

	// Create and return the new HTTP client potentially configured with TLS.
	client := http.Client{
		Transport: &http.Transport{
			Dial:            dial,
			TLSClientConfig: tlsConfig,
		},
	}
	return &client, nil
}
