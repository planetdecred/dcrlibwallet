package dcrlibwallet

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	www "github.com/decred/politeia/politeiawww/api/www/v1"
	"golang.org/x/sync/errgroup"
)

const (
	updateInterval = 10 // 10 mins
)

func (p *Politeia) Sync() error {
	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		return p.sync()
	})

	return g.Wait()
}

func (p *Politeia) sync() error {
	log.Info("Politeia sync: started")

	// fetch server policy if it's not been fetched
	if p.client.policy == nil {
		serverPolicy, err := p.client.serverPolicy()
		if err != nil {
			return err
		}
		p.client.policy = &serverPolicy
	}

	var savedTokens []string
	err := p.db.Select(q.True()).Each(new(Proposal), func(record interface{}) error {
		p := record.(*Proposal)
		savedTokens = append(savedTokens, p.CensorshipRecord.Token)
		return nil
	})
	if err != nil && err != storm.ErrNotFound {
		return fmt.Errorf("error loading saved proposals: %s", err.Error())
	}

	// fetch remote token inventory
	log.Info("Politeia sync: fetching token inventory")
	tokenInventory, err := p.client.tokenInventory()
	if err != nil {
		return err
	}

	err = p.fetchAllUnfetchedProposals(tokenInventory, savedTokens)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(updateInterval * time.Minute)

	go func() {
		for {
			select {
			case <-ticker.C:
				log.Info("Politeia sync: checking for proposal updates")
				p.checkForUpdates()
			case <-p.quitChan:
				ticker.Stop()
				return
			}
		}
	}()
	<-p.quitChan

	return nil
}

func (p *Politeia) CancelSync() {
	close(p.quitChan)
	log.Info("Politeia sync: stopped")
}

func (p *Politeia) fetchAllUnfetchedProposals(tokenInventory *proposalTokenInventory, savedTokens []string) error {
	inventoryMap := map[int32][]string{
		ProposalCategoryPre:       p.getUniqueTokens(tokenInventory.Pre, savedTokens),
		ProposalCategoryActive:    p.getUniqueTokens(tokenInventory.Active, savedTokens),
		ProposalCategoryApproved:  p.getUniqueTokens(tokenInventory.Approved, savedTokens),
		ProposalCategoryRejected:  p.getUniqueTokens(tokenInventory.Rejected, savedTokens),
		ProposalCategoryAbandoned: p.getUniqueTokens(tokenInventory.Abandoned, savedTokens),
	}

	totalNumProposalsToFetch := 0
	for _, v := range inventoryMap {
		totalNumProposalsToFetch += len(v)
	}

	if totalNumProposalsToFetch > 0 {
		log.Infof("Politeia sync: fetching %d new proposals", totalNumProposalsToFetch)
	} else {
		log.Infof("Politeia sync: no new proposals found. Checking again in %d minutes", updateInterval)
		return nil
	}

	for k, v := range inventoryMap {
		err := p.syncBatchProposals(k, v)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Politeia) syncBatchProposals(category int32, proposalsInventory []string) error {
	for {
		if len(proposalsInventory) == 0 {
			break
		}

		limit := p.client.policy.ProposalListPageSize
		if len(proposalsInventory) <= p.client.policy.ProposalListPageSize {
			limit = len(proposalsInventory)
		}

		var batch []string
		batch, proposalsInventory = proposalsInventory[:limit], proposalsInventory[limit:]

		batchTokens := &proposalTokens{batch}
		proposals, err := p.client.batchProposals(batchTokens)
		if err != nil {
			return err
		}

		votesSummaries, err := p.client.batchVoteSummary(batchTokens)
		if err != nil {
			return err
		}

		for i := range proposals {
			proposals[i].Category = category
			proposals[i].Token = proposals[i].CensorshipRecord.Token
			if voteSummary, ok := votesSummaries[proposals[i].CensorshipRecord.Token]; ok {
				voteSummary.YesCount, voteSummary.NoCount = p.getVotesCount(voteSummary.OptionsResult)
				proposals[i].VoteSummary = voteSummary
			}

			err = p.db.Save(&proposals[i])
			if err != nil {
				return fmt.Errorf("error saving new proposal: %s", err.Error())
			}
			p.publishNewProposal(&proposals[i])
		}
	}

	return nil
}

func (p *Politeia) checkForUpdates() error {
	startID := 0
	limit := p.client.policy.ProposalListPageSize

	var allProposals []Proposal

	for {
		var proposals []Proposal
		err := p.db.Range("ID", startID+1, startID+limit, &proposals)
		if err != nil && err != storm.ErrNotFound {
			return err
		}

		if len(proposals) == 0 {
			break
		}

		err = p.handleProposalsUpdate(proposals)
		if err != nil {
			return err
		}

		allProposals = append(allProposals, proposals...)
		startID = proposals[len(proposals)-1].ID
	}

	err := p.handleNewProposals(allProposals)
	if err != nil {
		return err
	}

	return nil
}

func (p *Politeia) handleProposalsUpdate(proposals []Proposal) error {
	tokens := make([]string, len(proposals))
	for i := range proposals {
		tokens[i] = proposals[i].CensorshipRecord.Token
	}

	batchTokens := &proposalTokens{tokens}
	batchProposals, err := p.client.batchProposals(batchTokens)
	if err != nil {
		return err
	}

	batchVotesSummaries, err := p.client.batchVoteSummary(batchTokens)
	if err != nil {
		return err
	}

	for i := range batchProposals {
		if voteSummary, ok := batchVotesSummaries[batchProposals[i].CensorshipRecord.Token]; ok {
			batchProposals[i].VoteSummary = voteSummary
		}

		for k := range proposals {
			if proposals[k].CensorshipRecord.Token == batchProposals[i].CensorshipRecord.Token {
				err := p.updateProposalDetails(proposals[k], batchProposals[i])
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (p *Politeia) updateProposalDetails(oldProposal, updatedProposal Proposal) error {
	if reflect.DeepEqual(oldProposal, updatedProposal) {
		return nil
	}
	updatedProposal.ID = oldProposal.ID
	updatedProposal.VoteSummary.YesCount, updatedProposal.VoteSummary.NoCount = p.getVotesCount(updatedProposal.VoteSummary.OptionsResult)

	var callback func(*Proposal)

	if oldProposal.Status != updatedProposal.Status && www.PropStatusT(updatedProposal.Status) == www.PropStatusAbandoned {
		updatedProposal.Category = ProposalCategoryAbandoned
	} else if oldProposal.VoteSummary.Status != updatedProposal.VoteSummary.Status {
		switch www.PropVoteStatusT(updatedProposal.VoteSummary.Status) {
		case www.PropVoteStatusFinished:
			callback = p.publishVoteFinished
			if updatedProposal.VoteSummary.Approved {
				updatedProposal.Category = ProposalCategoryApproved
			} else {
				updatedProposal.Category = ProposalCategoryRejected
			}
		case www.PropVoteStatusStarted:
			callback = p.publishVoteStarted
			updatedProposal.Category = ProposalCategoryActive
		default:
			updatedProposal.Category = ProposalCategoryPre
		}
	}

	err := p.db.Update(&updatedProposal)
	if err != nil {
		return fmt.Errorf("error saving updated proposal: %s", err.Error())
	}

	if callback != nil {
		callback(&updatedProposal)
	}

	return nil
}

func (p *Politeia) AddNotificationListener(notificationListener ProposalNotificationListener, uniqueIdentifier string) error {
	p.notificationListenersMu.Lock()
	defer p.notificationListenersMu.Unlock()

	if _, ok := p.notificationListeners[uniqueIdentifier]; ok {
		return errors.New(ErrListenerAlreadyExist)
	}

	p.notificationListeners[uniqueIdentifier] = notificationListener
	return nil
}

func (p *Politeia) RemoveNotificationListener(uniqueIdentifier string) {
	p.notificationListenersMu.Lock()
	defer p.notificationListenersMu.Unlock()

	delete(p.notificationListeners, uniqueIdentifier)
}

func (p *Politeia) publishNewProposal(proposal *Proposal) {
	if p.readConfigFunc(PoliteiaNotificationConfigKey, false) {
		p.notificationListenersMu.Lock()
		defer p.notificationListenersMu.Unlock()

		for _, notificationListener := range p.notificationListeners {
			notificationListener.OnNewProposal(proposal.ID, proposal.CensorshipRecord.Token)
		}
	}
}

func (p *Politeia) publishVoteStarted(proposal *Proposal) {
	if p.readConfigFunc(PoliteiaNotificationConfigKey, false) {
		p.notificationListenersMu.Lock()
		defer p.notificationListenersMu.Unlock()

		for _, notificationListener := range p.notificationListeners {
			notificationListener.OnProposalVoteStarted(proposal.ID, proposal.CensorshipRecord.Token)
		}
	}
}

func (p *Politeia) publishVoteFinished(proposal *Proposal) {
	if p.readConfigFunc(PoliteiaNotificationConfigKey, false) {
		p.notificationListenersMu.Lock()
		defer p.notificationListenersMu.Unlock()

		for _, notificationListener := range p.notificationListeners {
			notificationListener.OnProposalVoteFinished(proposal.ID, proposal.CensorshipRecord.Token)
		}
	}
}

func (p *Politeia) getVotesCount(options []ProposalVoteOptionResult) (int32, int32) {
	var yes, no int32

	for i := range options {
		if options[i].Option.ID == "yes" {
			yes = int32(options[i].VotesReceived)
		} else {
			no = int32(options[i].VotesReceived)
		}
	}

	return yes, no
}

func (p *Politeia) handleNewProposals(proposals []Proposal) error {
	loadedTokens := make([]string, len(proposals))
	for i := range proposals {
		loadedTokens[i] = proposals[i].CensorshipRecord.Token
	}

	tokenInventory, err := p.client.tokenInventory()
	if err != nil {
		return err
	}

	return p.fetchAllUnfetchedProposals(tokenInventory, loadedTokens)
}

func (p *Politeia) getUniqueTokens(tokenInventory, savedTokens []string) []string {
	var diff []string

	for i := range tokenInventory {
		exists := false

		for k := range savedTokens {
			if savedTokens[k] == tokenInventory[i] {
				exists = true
				break
			}
		}

		if !exists {
			diff = append(diff, tokenInventory[i])
		}
	}

	return diff
}
