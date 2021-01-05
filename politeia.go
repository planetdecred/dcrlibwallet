package dcrlibwallet

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
)

type Politeia struct {
	client                  *politeiaClient
	db                      *storm.DB
	readConfigFunc          func(string, bool) bool
	notificationListenersMu sync.RWMutex
	notificationListeners   map[string]ProposalNotificationListener
	quitChan                chan struct{}
}

const (
	proposalsDbName = "proposals.db"
)

const (
	ProposalCategoryAll int32 = iota + 1
	ProposalCategoryPre
	ProposalCategoryActive
	ProposalCategoryApproved
	ProposalCategoryRejected
	ProposalCategoryAbandoned
)

func newPoliteia(rootDir string, readConfigFunc func(string, bool) bool) (*Politeia, error) {
	db, err := storm.Open(filepath.Join(rootDir, proposalsDbName))
	if err != nil {
		return nil, fmt.Errorf("error opening proposals database: %s", err.Error())
	}

	err = db.Init(&Proposal{})
	if err != nil {
		return nil, fmt.Errorf("error initializing proposals database: %s", err.Error())
	}

	p := &Politeia{
		client:                newPoliteiaClient(),
		db:                    db,
		readConfigFunc:        readConfigFunc,
		notificationListeners: make(map[string]ProposalNotificationListener),
	}

	return p, nil
}

func (p *Politeia) Shutdown() {
	//close(p.syncQuitChan)
	p.db.Close()
}

// GetProposalsRaw fetches and returns a proposals from the db
func (p *Politeia) GetProposalsRaw(category int32, offset, limit int32, newestFirst bool) ([]Proposal, error) {
	query := p.prepareQuery(category, offset, limit, newestFirst)

	var proposals []Proposal
	err := query.Find(&proposals)
	if err != nil && err != storm.ErrNotFound {
		return nil, fmt.Errorf("error fetching proposals: %s", err.Error())
	}

	return proposals, nil
}

// GetProposals returns the result of GetProposalsRaw as a JSON string
func (p *Politeia) GetProposals(category int32, offset, limit int32, newestFirst bool) (string, error) {

	result, err := p.GetProposalsRaw(category, offset, limit, newestFirst)
	if err != nil {
		return "", err
	}

	if len(result) == 0 {
		return "[]", nil
	}

	response, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("error marshalling result: %s", err.Error())
	}

	return string(response), nil
}

// GetProposalRaw fetches and returns a single proposal specified by it's censorship record token
func (p *Politeia) GetProposalRaw(censorshipToken string) (*Proposal, error) {
	var proposal Proposal
	err := p.db.One("Token", censorshipToken, &proposal)
	if err != nil {
		return nil, err
	}

	return &proposal, nil
}

// GetProposal returns the result of GetProposalRaw as a JSON string
func (p *Politeia) GetProposal(censorshipToken string) (string, error) {
	return p.marshalResult(p.GetProposalRaw(censorshipToken))
}

// GetProposalByIDRaw fetches and returns a single proposal specified by it's ID
func (p *Politeia) GetProposalByIDRaw(proposalID int) (*Proposal, error) {
	var proposal Proposal
	err := p.db.One("ID", proposalID, &proposal)
	if err != nil {
		return nil, err
	}

	return &proposal, nil
}

// GetProposalByID returns the result of GetProposalByIDRaw as a JSON string
func (p *Politeia) GetProposalByID(proposalID int) (string, error) {
	return p.marshalResult(p.GetProposalByIDRaw(proposalID))
}

// Count returns the number of proposals of a specified category
func (p *Politeia) Count(category int32) (int32, error) {
	var matcher q.Matcher

	if category == ProposalCategoryAll {
		matcher = q.True()
	} else {
		matcher = q.Eq("Category", category)
	}

	count, err := p.db.Select(matcher).Count(&Proposal{})
	if err != nil {
		return 0, err
	}

	return int32(count), nil
}

func (p *Politeia) prepareQuery(category int32, offset, limit int32, newestFirst bool) (query storm.Query) {
	switch category {
	case ProposalCategoryAll:
		query = p.db.Select(
			q.True(),
		)
	default:
		query = p.db.Select(
			q.Eq("Category", category),
		)
	}

	if offset > 0 {
		query = query.Skip(int(offset))
	}

	if limit > 0 {
		query = query.Limit(int(limit))
	}

	if newestFirst {
		query = query.OrderBy("Timestamp").Reverse()
	} else {
		query = query.OrderBy("Timestamp")
	}

	return
}

func (p *Politeia) marshalResult(result interface{}, err error) (string, error) {

	if err != nil {
		return "", translateError(err)
	}

	response, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("error marshalling result: %s", err.Error())
	}

	return string(response), nil
}
