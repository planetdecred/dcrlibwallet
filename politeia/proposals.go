package politeia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"
)

func (p *Politeia) getProposalsChunk(startHash string) ([]Proposal, error) {
	var queryStrings map[string]string
	if startHash != "" {
		queryStrings = map[string]string{
			"after": startHash,
		}
	}

	var result Proposals
	err := p.makeRequest(vettedProposalsPath, "GET", queryStrings, nil, &result)
	if err != nil {
		return nil, fmt.Errorf("error fetching proposals from %s: %v", startHash, err)
	}

	return result.Proposals, err
}

// GetProposalsChunk gets proposals starting after the proposal with the specified
// censorship hash. The number of proposals returned is specified in the poltieia
// policy API endpoint
func (p *Politeia) GetProposalsChunk(startHash string) (string, error) {
	proposals, err := p.getProposalsChunk(startHash)
	if err != nil {
		return "", err
	}

	wg, _ := errgroup.WithContext(context.Background())
	for i := range proposals {
		wg.Go(func() error {
			voteStatus, err := p.getVoteStatus(proposals[i].CensorshipRecord.Token)
			if err != nil {
				return err
			}
			proposals[i].VoteStatus = *voteStatus
			return nil
		})
	}

	if err := wg.Wait(); err != nil {
		return "", err
	}

	jsonBytes, err := json.Marshal(proposals)
	if err != nil {
		return "", fmt.Errorf("error marshalling proposal result to json: %s", err.Error())
	}

	return string(jsonBytes), nil
}

// GetAllProposal fetches all vetted proposals
func (p *Politeia) GetAllProposals() (string, error) {
	var proposalChunkResult, proposals []Proposal
	var err error

	if p.serverPolicy == nil {
		policy, err := p.getServerPolicy()
		if err != nil {
			return "", err
		}
		p.serverPolicy = policy
	}

	proposalChunkResult, err = p.getProposalsChunk("")
	if err != nil {
		return "", fmt.Errorf("error fetching all proposals: %s", err.Error())
	}
	proposals = append(proposals, proposalChunkResult...)

	for {
		if proposalChunkResult == nil || len(proposalChunkResult) < p.serverPolicy.ProposalListPageSize {
			break
		}

		proposalChunkResult, err = p.getProposalsChunk(proposalChunkResult[p.serverPolicy.ProposalListPageSize-1].CensorshipRecord.Token)
		if err != nil {
			return "", err
		}
		proposals = append(proposals, proposalChunkResult...)
	}

	jsonBytes, err := json.Marshal(proposals)
	if err != nil {
		return "", fmt.Errorf("error marshalling proposal result to json: %s", err.Error())
	}

	return string(jsonBytes), err
}

// GetProposalDetails fetches the details of a single proposal
// if the version argument is an empty string, the latest version is used
func (p *Politeia) GetProposalDetails(censorshipToken, version string) (string, error) {
	if censorshipToken == "" {
		return "", errors.New("censorship token cannot be empty")
	}

	var queryStrings map[string]string
	if version != "" {
		queryStrings = map[string]string{
			"version": version,
		}
	}

	var result ProposalResult
	err := p.makeRequest(fmt.Sprintf(proposalDetailsPath, censorshipToken), "GET", queryStrings, nil, &result)
	if err != nil {
		return "", err
	}

	jsonBytes, err := json.Marshal(result.Proposal)
	if err != nil {
		return "", fmt.Errorf("error marshalling proposal result to json: %s", err.Error())
	}

	return string(jsonBytes), err
}

func (p *Politeia) getVoteStatus(censorshipToken string) (*VoteStatus, error) {
	if censorshipToken == "" {
		return nil, errors.New("censorship token cannot be empty")
	}

	var voteStatus VoteStatus
	err := p.makeRequest(fmt.Sprintf(voteStatusPath, censorshipToken), "GET", nil, nil, &voteStatus)
	if err != nil {
		return nil, err
	}

	return &voteStatus, nil
}

// GetVoteStatus fetches the vote status of a single public proposal
func (p *Politeia) GetVoteStatus(censorshipToken string) (string, error) {
	voteStatus, err := p.GetVoteStatus(censorshipToken)
	if err != nil {
		return "", err
	}

	jsonBytes, err := json.Marshal(voteStatus)
	if err != nil {
		return "", fmt.Errorf("error marshalling proposal result to json: %s", err.Error())
	}

	return string(jsonBytes), nil
}

// GetAllVotesStatus fetches the vote status of all public proposals
func (p *Politeia) GetAllVotesStatus() (string, error) {
	var votesStatus VotesStatus
	err := p.makeRequest(votesStatusPath, "GET", nil, nil, &votesStatus)
	if err != nil {
		return "", err
	}

	jsonBytes, err := json.Marshal(votesStatus)
	if err != nil {
		return "", fmt.Errorf("error marshalling proposal result to json: %s", err.Error())
	}

	return string(jsonBytes), nil
}
