package dcrlibwallet

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	proposalsAPIEndpoint = "https://proposals.decred.org/api/v1/proposals/vetted"
)

func (mw *MultiWallet) createHTTPClient() {
	mw.httpClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 2,
		},
		Timeout: time.Duration(10) * time.Second,
	}
}

func (mw *MultiWallet) GetPoliteaProposals() ([]Proposal, error) {
	res, err := mw.httpClient.Get(proposalsAPIEndpoint)
	if err != nil {
		return nil, fmt.Errorf("error fetching proposals from endpoint: %v", err)
	}
	defer res.Body.Close()

	var proposalsResult *ProposalResult
	err = json.NewDecoder(res.Body).Decode(&proposalsResult)
	if err != nil {
		return nil, fmt.Errorf("error decoding json response: %v", err)
	}

	return proposalsResult.Proposals, nil
}
