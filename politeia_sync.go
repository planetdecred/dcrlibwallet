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
	retryInterval = 15 // 15 seconds
)

// Sync fetches all proposals from the server and
func (p *Politeia) Sync() error {

	p.mu.Lock()

	if p.cancelSync != nil {
		p.mu.Unlock()
		return errors.New(ErrSyncAlreadyInProgress)
	}

	log.Info("Politeia sync: started")

	p.ctx, p.cancelSync = p.mwRef.contextWithShutdownCancel()
	p.client = newPoliteiaClient()
	defer p.resetSyncData()

	p.mu.Unlock()

	for {
		// fetch server policy if it's not been fetched
		if p.client.policy == nil {
			err := p.client.loadServerPolicy()
			if err != nil {
				log.Errorf("Error fetching for politeia server policy: %v", err)
				time.Sleep(retryInterval * time.Second)
				continue
			}
		}

		if done(p.ctx) {
			return errors.New(ErrContextCanceled)
		}

		log.Info("Politeia sync: checking for updates")

		err := p.checkForUpdates()
		if err != nil {
			log.Errorf("Error checking for politeia updates: %v", err)
			time.Sleep(retryInterval * time.Second)
			continue
		}

		log.Info("Politeia sync: update complete")
		p.publishSynced()
		return nil
	}
}

func (p *Politeia) IsSyncing() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cancelSync != nil
}

// this function requres p.mu unlocked.
func (p *Politeia) resetSyncData() {
	p.cancelSync = nil
}

func (p *Politeia) StopSync() {
	p.mu.Lock()
	if p.cancelSync != nil {
		p.cancelSync()
		p.resetSyncData()
	}
	p.mu.Unlock()
	log.Info("Politeia sync: stopped")
}

func (p *Politeia) checkForUpdates() error {
	offset := 0
	p.mu.RLock()
	limit := int32(p.client.policy.ProposalListPageSize)
	p.mu.RUnlock()

	for {
		proposals, err := p.getProposalsRaw(ProposalCategoryAll, int32(offset), limit, true, true)
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
	}

	// include abandoned proposals
	allProposals, err := p.getProposalsRaw(ProposalCategoryAll, 0, 0, true, false)
	if err != nil && err != storm.ErrNotFound {
		return err
	}

	err = p.handleNewProposals(allProposals)
	if err != nil {
		return err
	}

	return nil
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
			batchProposals[i].QuorumPercentage = int32(voteSummary.QuorumPercentage)
			batchProposals[i].YesVotes, batchProposals[i].NoVotes = getVotesCount(voteSummary.Results)
		}

		for k := range proposals {
			if proposals[k].Token == batchProposals[i].Token {

				// proposal category
				batchProposals[i].Category = proposals[k].Category
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
	updatedProposal.ID = oldProposal.ID

	if reflect.DeepEqual(oldProposal, updatedProposal) {
		return nil
	}

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

func (p *Politeia) fetchAllUnfetchedProposals(tokenInventory *www.TokenInventoryReply, savedTokens []string) error {

	broadcastNotification := len(savedTokens) > 0

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
		log.Infof("Politeia sync: no new proposals found")
		return nil
	}

	if done(p.ctx) {
		return errors.New(ErrContextCanceled)
	}

	for category, tokens := range inventoryMap {
		err := p.fetchBatchProposals(category, tokens, broadcastNotification)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Politeia) fetchBatchProposals(category int32, tokens []string, broadcastNotification bool) error {
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
				proposals[i].QuorumPercentage = int32(voteSummary.QuorumPercentage)
				proposals[i].YesVotes, proposals[i].NoVotes = getVotesCount(voteSummary.Results)
			}

			err = p.saveOrOverwiteProposal(&proposals[i])
			if err != nil {
				return fmt.Errorf("error saving new proposal: %s", err.Error())
			}

			p.mu.RLock()
			if broadcastNotification {
				p.publishNewProposal(&proposals[i])
			}
			p.mu.RUnlock()
		}

		log.Infof("Politeia sync: fetched %d proposals", limit)
	}

	return nil
}

func (p *Politeia) FetchProposalDescription(token string) (string, error) {

	proposal, err := p.GetProposalRaw(token)
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	if client == nil {
		client = newPoliteiaClient()
		err := client.loadServerPolicy()
		if err != nil {
			return "", err
		}
	}

	proposalDetailsReply, err := client.proposalDetails(token)
	if err != nil {
		return "", err
	}

	for _, file := range proposalDetailsReply.Proposal.Files {
		if file.Name == "index.md" {
			b, err := DecodeBase64(file.Payload)
			if err != nil {
				return "", err
			}

			// save file to db
			proposal.IndexFile = string(b)
			// index file version will be used to determine if the
			// saved file is out of date when compared to version.
			proposal.IndexFileVersion = proposal.Version
			err = p.saveOrOverwiteProposal(proposal)
			if err != nil {
				log.Errorf("error saving new proposal: %s", err.Error())
			}

			return proposal.IndexFile, nil
		}
	}

	return "", errors.New(ErrNotExist)
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
