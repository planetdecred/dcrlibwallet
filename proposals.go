package dcrlibwallet

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	proposalsEndpoint = "https://proposals.decred.org/api/v1/proposals/vetted"
	policyEndpoint    = "https://proposals.decred.org/api/v1/policy"
)

func (mw *MultiWallet) createHTTPClient() {
	mw.httpClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 2,
		},
		Timeout: time.Duration(10) * time.Second,
	}
}

func (mw *MultiWallet) get(url string, responseData interface{}) error {
	res, err := mw.httpClient.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return json.NewDecoder(res.Body).Decode(responseData)
}

func (mw *MultiWallet) getPolicy() (*Policy, error) {
	var policy *Policy
	err := mw.get(policyEndpoint, &policy)
	if err != nil {
		return nil, fmt.Errorf("error fetching politea policy: %v", err)
	}

	return policy, nil
}

func (mw *MultiWallet) GetProposalsChunk(from string) ([]Proposal, error) {
	var result Proposals
	url := proposalsEndpoint
	if from != "" {
		url = fmt.Sprintf(proposalsEndpoint+"?from=%s", from)
	}

	err := mw.get(url, &result)
	if err != nil {
		return nil, fmt.Errorf("error fetching proposals from %s: %v", from, err)
	}

	return result.Proposals, nil

}

func (mw *MultiWallet) GetProposals() ([]Proposal, error) {
	var proposals []Proposal
	var result []Proposal
	var err error

	policy, err := mw.getPolicy()
	if err != nil {
		return nil, err
	}

	result, err = mw.GetProposalsChunk("")
	if err != nil {
		return nil, fmt.Errorf("error fetching all proposals")
	}
	proposals = append(proposals, result...)

	for {
		if result == nil || len(result) < policy.ProposalListPageSize {
			break
		}

		result, err = mw.GetProposalsChunk(result[policy.ProposalListPageSize-1].CensorshipRecord.Token)
		if err != nil {
			break
		}

		proposals = append(proposals, result...)
	}

	return proposals, err
}
