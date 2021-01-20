package dcrlibwallet

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/asdine/storm"
	www "github.com/decred/politeia/politeiawww/api/www/v1"
)

const (
	updateInterval = 30 // 30 mins
)

// Sync fetches all proposals from the server and
func (p *Politeia) Sync() error {

	p.mu.Lock()

	if p.client != nil {
		p.mu.Unlock()
		return errors.New(ErrSyncAlreadyInProgress)
	}

	log.Info("Politeia sync: started")

	p.ctx, p.cancelSync = p.mwRef.contextWithShutdownCancel()
	p.client = newPoliteiaClient()

	p.mu.Unlock()

	// fetch server policy if it's not been fetched
	if p.client.policy == nil {
		serverPolicy, err := p.client.serverPolicy()
		if err != nil {
			return err
		}
		p.client.policy = &serverPolicy
	}

	if done(p.ctx) {
		return errors.New(ErrContextCanceled)
	}

	proposals, err := p.GetProposalsRaw(ProposalCategoryAll, 0, 0, true)
	if err != nil && err != storm.ErrNotFound {
		return fmt.Errorf("error loading saved proposals: %s", err.Error())
	}

	savedTokens := make([]string, len(proposals))
	for i, proposal := range proposals {
		savedTokens[i] = proposal.Token
	}

	if done(p.ctx) {
		return errors.New(ErrContextCanceled)
	}

	// fetch remote token inventory
	log.Info("Politeia sync: fetching token inventory")
	tokenInventory, err := p.client.tokenInventory()
	if err != nil {
		return err
	}

	if done(p.ctx) {
		return errors.New(ErrContextCanceled)
	}

	log.Info("Politeia sync: fetching missing proposals")
	err = p.fetchAllUnfetchedProposals(tokenInventory, savedTokens)
	if err != nil {
		return err
	}

	if done(p.ctx) {
		return errors.New(ErrContextCanceled)
	}

	log.Info("Politeia sync: synced")
	p.mu.Lock()
	p.synced = true
	p.mu.Unlock()
	p.publishSynced()

	go func() {
		ticker := time.NewTicker(updateInterval * time.Minute)

		go func() {
			for {
				select {
				case <-ticker.C:
					log.Info("Politeia sync: checking for proposal updates")
					p.checkForUpdates()
				case <-p.ctx.Done():
					ticker.Stop()
					return
				}
			}
		}()
	}()

	log.Info("Politeia sync: fetch complete")
	return nil
}

func (p *Politeia) IsConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cancelSync != nil
}

func (p *Politeia) Synced() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.synced
}

func (p *Politeia) StopSync() {
	p.mu.Lock()
	if p.cancelSync != nil {
		p.cancelSync()
		p.synced = false
		p.client = nil
		p.cancelSync = nil
	}
	p.mu.Unlock()
	log.Info("Politeia sync: stopped")
}

func (p *Politeia) fetchAllUnfetchedProposals(tokenInventory *www.TokenInventoryReply, savedTokens []string) error {

	approvedTokens, savedTokens := p.getUniqueTokens(tokenInventory.Approved, savedTokens)
	rejectedTokens, savedTokens := p.getUniqueTokens(tokenInventory.Rejected, savedTokens)
	abandonedTokens, savedTokens := p.getUniqueTokens(tokenInventory.Abandoned, savedTokens)
	preTokens, savedTokens := p.getUniqueTokens(tokenInventory.Pre, savedTokens)
	activeTokens, _ := p.getUniqueTokens(tokenInventory.Active, savedTokens)

	inventoryMap := map[int32][]string{
		ProposalCategoryPre:       preTokens,
		ProposalCategoryActive:    activeTokens,
		ProposalCategoryApproved:  approvedTokens,
		ProposalCategoryRejected:  rejectedTokens,
		ProposalCategoryAbandoned: abandonedTokens,
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

	if done(p.ctx) {
		return errors.New(ErrContextCanceled)
	}

	for category, tokens := range inventoryMap {
		err := p.fetchBatchProposals(category, tokens)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Politeia) fetchBatchProposals(category int32, tokens []string) error {
	for {
		if len(tokens) == 0 {
			break
		}

		p.mu.RLock()
		if done(p.ctx) {
			return errors.New(ErrContextCanceled)
		}

		limit := int(p.client.policy.ProposalListPageSize)
		if len(tokens) <= limit {
			limit = len(tokens)
		}

		p.mu.RUnlock()

		var tokenBatch []string
		tokenBatch, tokens = tokens[:limit], tokens[limit:]

		proposals, err := p.client.batchProposals(tokenBatch)
		if err != nil {
			return err
		}

		if done(p.ctx) {
			return errors.New(ErrContextCanceled)
		}

		votesSummaries, err := p.client.batchVoteSummary(tokenBatch)
		if err != nil {
			return err
		}

		if done(p.ctx) {
			return errors.New(ErrContextCanceled)
		}

		for i := range proposals {
			proposals[i].Category = category
			if voteSummary, ok := votesSummaries[proposals[i].Token]; ok {
				proposals[i].VoteStatus = int32(voteSummary.Status)
				proposals[i].VoteApproved = voteSummary.Approved
				proposals[i].PassPercentage = int32(voteSummary.PassPercentage)
				proposals[i].EligibleTickets = int32(voteSummary.EligibleTickets)
				proposals[i].YesVotes, proposals[i].NoVotes = getVotesCount(voteSummary.Results)
			}

			err = p.saveOrOverwiteProposal(&proposals[i])
			if err != nil {
				return fmt.Errorf("error saving new proposal: %s", err.Error())
			}

			p.mu.RLock()
			if p.synced {
				p.publishNewProposal(&proposals[i])
			}
			p.mu.RUnlock()
		}

		log.Infof("Politeia sync: fetched %d proposals", limit)
	}

	return nil
}

func (p *Politeia) checkForUpdates() error {
	offset := 0
	p.mu.RLock()
	limit := int32(p.client.policy.ProposalListPageSize)
	p.mu.RUnlock()

	var allProposals []Proposal

	for {
		proposals, err := p.GetProposalsRaw(ProposalCategoryAll, int32(offset), limit, true)
		if err != nil && err != storm.ErrNotFound {
			return err
		}

		if len(proposals) == 0 {
			break
		}

		offset += len(proposals)

		err = p.handleProposalsUpdate(proposals)
		if err != nil {
			return err
		}

		allProposals = append(allProposals, proposals...)
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
		tokens[i] = proposals[i].Token
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	batchProposals, err := p.client.batchProposals(tokens)
	if err != nil {
		return err
	}

	batchVotesSummaries, err := p.client.batchVoteSummary(tokens)
	if err != nil {
		return err
	}

	for i := range batchProposals {
		if voteSummary, ok := batchVotesSummaries[batchProposals[i].Token]; ok {
			batchProposals[i].VoteStatus = int32(voteSummary.Status)
			batchProposals[i].VoteApproved = voteSummary.Approved
			batchProposals[i].PassPercentage = int32(voteSummary.PassPercentage)
			batchProposals[i].EligibleTickets = int32(voteSummary.EligibleTickets)
			batchProposals[i].YesVotes, proposals[i].NoVotes = getVotesCount(voteSummary.Results)
		}

		for k := range proposals {
			if proposals[k].Token == batchProposals[i].Token {
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

	var callback func(*Proposal)

	if oldProposal.Status != updatedProposal.Status && www.PropStatusT(updatedProposal.Status) == www.PropStatusAbandoned {
		updatedProposal.Category = ProposalCategoryAbandoned
	} else if oldProposal.VoteStatus != updatedProposal.VoteStatus {
		switch www.PropVoteStatusT(updatedProposal.VoteStatus) {
		case www.PropVoteStatusFinished:
			callback = p.publishVoteFinished
			if updatedProposal.VoteApproved {
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

	err := p.mwRef.db.Update(&updatedProposal)
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

func (p *Politeia) publishSynced() {
	p.notificationListenersMu.Lock()
	defer p.notificationListenersMu.Unlock()

	for _, notificationListener := range p.notificationListeners {
		notificationListener.OnProposalsSynced()
	}
}

func (p *Politeia) publishNewProposal(proposal *Proposal) {
	p.notificationListenersMu.Lock()
	defer p.notificationListenersMu.Unlock()

	for _, notificationListener := range p.notificationListeners {
		notificationListener.OnNewProposal(proposal)
	}
}

func (p *Politeia) publishVoteStarted(proposal *Proposal) {
	p.notificationListenersMu.Lock()
	defer p.notificationListenersMu.Unlock()

	for _, notificationListener := range p.notificationListeners {
		notificationListener.OnProposalVoteStarted(proposal)
	}
}

func (p *Politeia) publishVoteFinished(proposal *Proposal) {
	p.notificationListenersMu.Lock()
	defer p.notificationListenersMu.Unlock()

	for _, notificationListener := range p.notificationListeners {
		notificationListener.OnProposalVoteFinished(proposal)
	}
}

func getVotesCount(options []www.VoteOptionResult) (int32, int32) {
	var yes, no int32

	for i := range options {
		if options[i].Option.Id == "yes" {
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
		loadedTokens[i] = proposals[i].Token
	}

	p.mu.RLock()
	tokenInventory, err := p.client.tokenInventory()
	p.mu.RUnlock()
	if err != nil {
		return err
	}

	return p.fetchAllUnfetchedProposals(tokenInventory, loadedTokens)
}

func (p *Politeia) getUniqueTokens(tokenInventory, savedTokens []string) ([]string, []string) {
	var diff []string

	for i := range tokenInventory {
		exists := false

		for k := range savedTokens {
			if savedTokens[k] == tokenInventory[i] {
				exists = true
				savedTokens = append(savedTokens[:k], savedTokens[k+1:]...)
				break
			}
		}

		if !exists {
			diff = append(diff, tokenInventory[i])
		}
	}

	return diff, savedTokens
}
