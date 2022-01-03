package dcrlibwallet

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrwallet/v2/errors"
	w "decred.org/dcrwallet/v2/wallet"
	"github.com/asdine/storm"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/asdine/storm/q"
)

type Consensus struct {
	mwRef                   *MultiWallet
	mu                      sync.RWMutex
	ctx                     context.Context
	cancelSync              context.CancelFunc
	notificationListenersMu sync.RWMutex
	notificationListeners   map[string]ConsensusNotificationListener
}

const (
	ConsensusLastSyncedTimestampConfigKey = "consensus_last_synced_timestamp"
)

func newConsensus(mwRef *MultiWallet) (*Consensus, error) {
	c := &Consensus{
		mwRef:                 mwRef,
		notificationListeners: make(map[string]ConsensusNotificationListener),
	}

	return c, nil
}

func (c *Consensus) saveLastSyncedTimestamp(lastSyncedTimestamp int64) {
	c.mwRef.SetLongConfigValueForKey(ConsensusLastSyncedTimestampConfigKey, lastSyncedTimestamp)
}

func (c *Consensus) getLastSyncedTimestamp() int64 {
	return c.mwRef.ReadLongConfigValueForKey(ConsensusLastSyncedTimestampConfigKey, 0)
}

func (c *Consensus) GetLastSyncedTimeStamp() int64 {
	return c.getLastSyncedTimestamp()
}

func (c *Consensus) Sync(wallets []*Wallet) error {

	c.mu.Lock()

	if c.cancelSync != nil {
		c.mu.Unlock()
		return errors.New(ErrSyncAlreadyInProgress)
	}

	log.Info("Consensus sync: started")

	c.ctx, c.cancelSync = c.mwRef.contextWithShutdownCancel()
	defer c.resetSyncData()

	c.mu.Unlock()

	for i, w := range wallets {

		log.Info("fires i: ", i, w.ID)
		_, err := c.getAllAgendasForWallet(w.ID)
		if err != nil {
			log.Errorf("Error fetching agendas: %v", err)
			time.Sleep(retryInterval * time.Second)
			continue
		}

		if done(c.ctx) {
			return errors.New(ErrContextCanceled)
		}

		log.Info("Consensus sync: checking for updates")

		// err = c.checkForUpdates()
		// if err != nil {
		// 	log.Errorf("Error checking for consensus updates: %v", err)
		// 	time.Sleep(retryInterval * time.Second)
		// 	continue
		// }

		log.Info("Consensus sync: update complete")
		c.saveLastSyncedTimestamp(time.Now().Unix())
		c.publishSynced()

	}
	return nil
}

func (c *Consensus) IsSyncing() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cancelSync != nil
}

// this function requres c.mu unlocked.
func (c *Consensus) resetSyncData() {
	c.cancelSync = nil
}

func (c *Consensus) StopSync() {
	c.mu.Lock()
	if c.cancelSync != nil {
		c.cancelSync()
		c.resetSyncData()
	}
	c.mu.Unlock()
	log.Info("Consensus sync: stopped")
}

func (c *Consensus) AddNotificationListener(notificationListener ConsensusNotificationListener, uniqueIdentifier string) error {
	c.notificationListenersMu.Lock()
	defer c.notificationListenersMu.Unlock()

	if _, ok := c.notificationListeners[uniqueIdentifier]; ok {
		return errors.New(ErrListenerAlreadyExist)
	}

	c.notificationListeners[uniqueIdentifier] = notificationListener
	return nil
}

func (c *Consensus) RemoveNotificationListener(uniqueIdentifier string) {
	c.notificationListenersMu.Lock()
	defer c.notificationListenersMu.Unlock()

	delete(c.notificationListeners, uniqueIdentifier)
}

func (c *Consensus) publishSynced() {
	c.notificationListenersMu.Lock()
	defer c.notificationListenersMu.Unlock()

	for _, notificationListener := range c.notificationListeners {
		notificationListener.OnAgendasSynced()
	}
}

func (c *Consensus) publishNewAgenda(agenda *Agenda) {
	c.notificationListenersMu.Lock()
	defer c.notificationListenersMu.Unlock()

	for _, notificationListener := range c.notificationListeners {
		notificationListener.OnNewAgenda(agenda)
	}
}

func (c *Consensus) publishAgendaVoteStarted(agenda *Agenda) {
	c.notificationListenersMu.Lock()
	defer c.notificationListenersMu.Unlock()

	for _, notificationListener := range c.notificationListeners {
		notificationListener.OnAgendaVoteStarted(agenda)
	}
}

func (c *Consensus) publishAgendaVoteFinished(agenda *Agenda) {
	c.notificationListenersMu.Lock()
	defer c.notificationListenersMu.Unlock()

	for _, notificationListener := range c.notificationListeners {
		notificationListener.OnAgendaVoteFinished(agenda)
	}
}

// GetVoteChoices handles a getvotechoices request by returning configured vote
// preferences for each agenda of the latest supported stake version.
func (c *Consensus) GetVoteChoices(ctx context.Context, hash string, walletID int) (*GetVoteChoicesResult, error) {
	wallet := c.mwRef.WalletWithID(walletID)
	wal := wallet.Internal()
	if wal == nil {
		return nil, fmt.Errorf("request requires a wallet but wallet has not loaded yet")
	}

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return nil, fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

	version, agendas := getAllAgendas(wal.ChainParams())
	resp := &GetVoteChoicesResult{
		Version: version,
		Choices: make([]VoteChoice, len(agendas)),
	}

	choices, _, err := wal.AgendaChoices(ctx, ticketHash)
	if err != nil {
		return nil, err
	}

	for i := range choices {
		resp.Choices[i] = VoteChoice{
			AgendaID:          choices[i].AgendaID,
			WalletID:          wallet.ID,
			AgendaDescription: agendas[i].Vote.Description,
			ChoiceID:          choices[i].ChoiceID,
			ChoiceDescription: "", // Set below
		}
		for j := range agendas[i].Vote.Choices {
			if choices[i].ChoiceID == agendas[i].Vote.Choices[j].Id {
				resp.Choices[i].ChoiceDescription = agendas[i].Vote.Choices[j].Description
				break
			}
		}

		err := c.saveOrOverwiteVoteChoice(wallet.ID, &resp.Choices[i])
		if err != nil {
			return nil, err
		}
	}

	return resp, nil
}

// SetVoteChoice handles a setvotechoice request by modifying the preferred
// choice for a voting agenda.
//
// If a VSP host is configured in the application settings, the voting
// preferences will also be set with the VSP.
func (c *Consensus) SetVoteChoice(walletID int, vspHost, agendaID, choiceID, hash, passphrase string) error {
	wallet := c.mwRef.WalletWithID(walletID)
	wal := wallet.Internal()
	if wal == nil {
		return fmt.Errorf("request requires a wallet but wallet has not loaded yet")
	}

	err := wallet.UnlockWallet([]byte(passphrase))
	if err != nil {
		return translateError(err)
	}
	defer wallet.LockWallet()

	var ticketHash *chainhash.Hash
	if hash != "" {
		hash, err := chainhash.NewHashFromStr(hash)
		if err != nil {
			return fmt.Errorf("inavlid hash: %w", err)
		}
		ticketHash = hash
	}

	choice := w.AgendaChoice{
		AgendaID: agendaID,
		ChoiceID: choiceID,
	}

	var ctx context.Context
	_, err = wal.SetAgendaChoices(ctx, ticketHash, choice)
	if err != nil {
		return err
	}

	println("[][][][] A")
	_, err = c.getAllAgendasForWallet(walletID)
	if err != nil {
		return err
	}
	if vspHost == "" {
		return nil
	}
	println("[][][][] B")
	// vspClient, err := loader.LookupVSP(vspHost)
	// if err != nil {
	// 	return err
	// }
	// var c.mwRef MultiWallet
	vsp, err := c.mwRef.NewVSPClient(vspHost, walletID, 0)
	// vspClient, err := LookupVSP(vspHost)
	if err != nil {
		return err
	}
	if ticketHash != nil {
		err = vsp.SetVoteChoice(ctx, ticketHash, choice)
		return err
	}
	println("[][][][] 1")
	var firstErr error
	vsp.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		// Never return errors here, so all tickets are tried.
		// The first error will be returned to the user.
		println("[][][][] 2")
		err := vsp.SetVoteChoice(ctx, hash, choice)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		println("[][][][] 3")
		_, err = c.getAllAgendasForWallet(walletID)
		if err != nil {
			return err
		}
		println("[][][][] 4")
		return nil
	})
	// _, err = c.mwRef.GetAllAgendas(walletID)
	// if err != nil {
	// 	return err
	// }
	return firstErr
}

// getAllAgendas returns all agendas through out the various stake versions for the active network and
// this version of the software, and all agendas defined by it.
func (c *Consensus) getAllAgendasForWallet(walletID int) (*AgendasResponse, error) {
	wallet := c.mwRef.WalletWithID(walletID)
	version, deployments := getAllAgendas(wallet.chainParams)
	resp := &AgendasResponse{
		Version: version,
		Agendas: make([]*Agenda, len(deployments)),
	}

	var ctx context.Context
	_, err := c.GetVoteChoices(ctx, "", wallet.ID)
	if err != nil {
		return nil, errors.Errorf("error getting vote choices: %s", err.Error())
	}

	for i := range deployments {
		d := &deployments[i]
		// fmt.Println("[][][][] Deployment.vote.id", d.Vote.Id)
		voteChoice, err := c.GetVoteChoiceRaw(wallet.ID, d.Vote.Id)
		// println("[][][] vote choice getting all agendas", voteChoice.ChoiceID)
		if err != nil && err != storm.ErrNotFound {
			return nil, errors.Errorf("error getting vote choice: %s", err.Error())
		}
		// fmt.Println("[][][][] vote choice ", voteChoice.ChoiceID)
		if voteChoice.ChoiceID == "" {
			voteChoice.ChoiceID = "abstain"
		}
		resp.Agendas[i] = &Agenda{
			AgendaID:         d.Vote.Id,
			WalletID:         wallet.ID,
			Description:      d.Vote.Description,
			Mask:             uint32(d.Vote.Mask),
			Choices:          make([]*Choice, len(d.Vote.Choices)),
			VotingPreference: voteChoice.ChoiceID,
			StartTime:        int64(d.StartTime),
			ExpireTime:       int64(d.ExpireTime),
		}
		for j := range d.Vote.Choices {
			choice := &d.Vote.Choices[j]
			resp.Agendas[i].Choices[j] = &Choice{
				Id:          choice.Id,
				Description: choice.Description,
				Bits:        uint32(choice.Bits),
				IsAbstain:   choice.IsAbstain,
				IsNo:        choice.IsNo,
			}
		}
		fmt.Println("saving agenda wallet id", resp.Agendas[i].WalletID, wallet.ID, resp.Agendas[i].AgendaID)
		c.saveOrOverwiteAgenda(wallet.ID, resp.Agendas[i])
	}
	return resp, nil
}

// GetAllAgendas returns all agendas through out the various stake versions for the active network and
// this version of the software, and all agendas defined by it.
// func (c *Consensus) GetAllAgendas(wallets []*Wallet) (*AgendasResponse, error) {
// 	var ctx context.Context
// 	resp := &AgendasResponse{}
// 	for _, w := range wallets {
// 		wallet := c.mwRef.WalletWithID(w.ID)
// 		version, deployments := getAllAgendas(wallet.chainParams)
// 		resp = &AgendasResponse{
// 			Version: version,
// 			Agendas: make([]*Agenda, len(deployments)),
// 		}

// 		// fmt.Println("[][][][] Deployments", deployments)
// 		_, err := c.GetVoteChoices(ctx, "", w.ID)
// 		if err != nil {
// 			return nil, errors.Errorf("error getting vote choices: %s", err.Error())
// 		}

// 		for i := range deployments {
// 			d := &deployments[i]
// 			// fmt.Println("[][][][] Deployment.vote.id", d.Vote.Id)
// 			voteChoice, err := c.GetVoteChoiceRaw(d.Vote.Id)
// 			// println("[][][] vote choice getting all agendas", voteChoice.ChoiceID)
// 			if err != nil && err != storm.ErrNotFound {
// 				return nil, errors.Errorf("error getting vote choice: %s", err.Error())
// 			}
// 			// fmt.Println("[][][][] vote choice ", voteChoice.ChoiceID)
// 			if voteChoice.ChoiceID == "" {
// 				voteChoice.ChoiceID = "abstain"
// 			}
// 			resp.Agendas[i] = &Agenda{
// 				AgendaID:         d.Vote.Id,
// 				WalletID:		w.ID,
// 				Description:      d.Vote.Description,
// 				Mask:             uint32(d.Vote.Mask),
// 				Choices:          make([]*Choice, len(d.Vote.Choices)),
// 				VotingPreference: voteChoice.ChoiceID,
// 				StartTime:        int64(d.StartTime),
// 				ExpireTime:       int64(d.ExpireTime),
// 			}
// 			for j := range d.Vote.Choices {
// 				choice := &d.Vote.Choices[j]
// 				resp.Agendas[i].Choices[j] = &Choice{
// 					Id:          choice.Id,
// 					Description: choice.Description,
// 					Bits:        uint32(choice.Bits),
// 					IsAbstain:   choice.IsAbstain,
// 					IsNo:        choice.IsNo,
// 				}
// 			}
// 			c.saveOrOverwiteAgenda(resp.Agendas[i])
// 		}
// 	}
// 	// fmt.Println("[][][][] printing resp", resp)
// 	return resp, nil
// }

func getAllAgendas(params *chaincfg.Params) (version uint32, agendas []chaincfg.ConsensusDeployment) {
	version = voteVersion(params)
	if params.Deployments == nil {
		return version, nil
	}

	var i uint32
	allAgendas := make([]chaincfg.ConsensusDeployment, 0)
	for i = 1; i <= version; i++ {
		currentAgendas := params.Deployments[i]
		for j := 0; j < len(currentAgendas); j++ {
			agenda := currentAgendas[j]
			allAgendas = append(allAgendas, agenda)
		}
	}
	return version, allAgendas
}

func (c *Consensus) saveOrOverwiteAgenda(walletID int, agenda *Agenda) error {
	var oldAgenda Agenda
	err := c.mwRef.db.Select(q.Eq("AgendaID", agenda.AgendaID), q.Eq("WalletID", walletID)).First(&oldAgenda)
	if err != nil && err != storm.ErrNotFound {
		return errors.Errorf("error checking if agenda was already indexed: %s", err.Error())
	}

	fmt.Println("[][][][] old agenda", oldAgenda)

	if oldAgenda.AgendaID != "" {
		// delete old record before saving new (if it exists)
		println("[][][][] Deleteing agenda")
		c.mwRef.db.DeleteStruct(oldAgenda)
	}

	return c.mwRef.db.Save(agenda)
}

// func (c *Consensus) saveOrOverwiteAgenda(agenda *Agenda) error {
// 	var oldAgenda Agenda
// 	err := c.mwRef.db.One("AgendaID", agenda.AgendaID, &oldAgenda)
// 	if err != nil && err != storm.ErrNotFound {
// 		return errors.Errorf("error checking if agenda was already indexed: %s", err.Error())
// 	}

// 	if oldAgenda.AgendaID != "" {
// 		// delete old record before saving new (if it exists)
// 		// println("[][][][] Deleteing agenda")
// 		c.mwRef.db.DeleteStruct(oldAgenda)
// 	}

// 	return c.mwRef.db.Save(agenda)
// }

func (c *Consensus) saveOrOverwiteVoteChoice(walletID int, voteChoice *VoteChoice) error {
	var oldVoteChoice VoteChoice
	err := c.mwRef.db.Select(q.Eq("AgendaID", voteChoice.AgendaID), q.Eq("WalletID", walletID)).First(&oldVoteChoice)
	if err != nil && err != storm.ErrNotFound {
		return errors.Errorf("error checking if voteChoice was already indexed: %s", err.Error())
	}

	if oldVoteChoice.AgendaID != "" {
		// delete old record before saving new (if it exists)
		// println("[][][][] Deleteing vote choice")
		c.mwRef.db.DeleteStruct(oldVoteChoice)
	}

	// println("[][][][] saving vote choice id", voteChoice.AgendaID, voteChoice.ChoiceID)
	return c.mwRef.db.Save(voteChoice)
}

// func (c *Consensus) saveOrOverwiteVoteChoice(voteChoice *VoteChoice) error {
// 	var oldVoteChoice VoteChoice
// 	err := c.mwRef.db.One("AgendaID", voteChoice.AgendaID, &oldVoteChoice)
// 	if err != nil && err != storm.ErrNotFound {
// 		return errors.Errorf("error checking if voteChoice was already indexed: %s", err.Error())
// 	}

// 	if oldVoteChoice.AgendaID != "" {
// 		// delete old record before saving new (if it exists)
// 		// println("[][][][] Deleteing vote choice")
// 		c.mwRef.db.DeleteStruct(oldVoteChoice)
// 	}

// 	// println("[][][][] saving vote choice id", voteChoice.AgendaID, voteChoice.ChoiceID)
// 	return c.mwRef.db.Save(voteChoice)
// }

func (c *Consensus) deleteAllAgendasWithWalletID(walletID int) error {
	agendas := make([]Agenda, 0)
	err := c.mwRef.db.Find("walletID", walletID, &agendas)
	if err != nil && err != storm.ErrNotFound {
		return errors.Errorf("error checking if agenda was already indexed: %s", err.Error())
	}

	for _, agenda := range agendas {
		c.mwRef.db.DeleteStruct(agenda)
	}
	return nil
}

// GetVoteChoiceRaw fetches and returns a single votechoice specified by it's #AgendaID
func (c *Consensus) GetVoteChoiceRaw(walletID int, agendaID string) (*VoteChoice, error) {
	var voteChoice VoteChoice
	err := c.mwRef.db.Select(q.Eq("AgendaID", agendaID), q.Eq("WalletID", walletID)).First(&voteChoice)
	// err := c.mwRef.db.One("AgendaID", agendaID, &voteChoice)
	if err != nil && err != storm.ErrNotFound {
		return nil, err
	}

	return &voteChoice, nil
}

// GetAgenda returns the result of GetVoteChoice as a JSON string
func (c *Consensus) GetVoteChoice(walletID int, agendaID string) (string, error) {
	return c.marshalResult(c.GetVoteChoiceRaw(walletID, agendaID))
}

// GetAllAgendasRaw fetches and returns all agendas from the db
func (c *Consensus) GetAllAgendasRaw(offset, limit int32, newestFirst bool) ([]Agenda, error) {
	return c.getAllAgendasRaw(offset, limit, newestFirst)
}

func (c *Consensus) getAllAgendasRaw(offset, limit int32, newestFirst bool) ([]Agenda, error) {

	var query storm.Query

	query = c.mwRef.db.Select(
		q.True(),
	)

	if offset > 0 {
		query = query.Skip(int(offset))
	}

	if limit > 0 {
		query = query.Limit(int(limit))
	}

	if newestFirst {
		query = query.OrderBy("StartTime").Reverse()
	} else {
		query = query.OrderBy("StartTime")
	}

	var agendas []Agenda
	err := query.Find(&agendas)
	if err != nil && err != storm.ErrNotFound {
		return nil, fmt.Errorf("error fetching agendas: %s", err.Error())
	}

	return agendas, nil
}

// // GetAllAgendas returns the result of GetAllAgendasRaw as a JSON string
// func (c *Consensus) GetAllAgendas(offset, limit int32, newestFirst bool) (string, error) {

// 	result, err := c.GetAllAgendasRaw(offset, limit, newestFirst)
// 	if err != nil {
// 		return "", err
// 	}

// 	if len(result) == 0 {
// 		return "[]", nil
// 	}

// 	response, err := json.Marshal(result)
// 	if err != nil {
// 		return "", fmt.Errorf("error marshalling result: %s", err.Error())
// 	}

// 	return string(response), nil
// }

// GetAgendasByWalletIDRaw fetches and returns all agendas belonging to the selceted wallet from the db
func (c *Consensus) GetAgendasByWalletIDRaw(walletID int, offset, limit int32, newestFirst bool) ([]Agenda, error) {
	return c.getAgendasByWalletIDRaw(walletID, offset, limit, newestFirst)
}

func (c *Consensus) getAgendasByWalletIDRaw(walletID int, offset, limit int32, newestFirst bool) ([]Agenda, error) {

	var query storm.Query

	// query = c.mwRef.db.Select(
	// 	q.True(),
	// )
	query = c.mwRef.db.Select(
		q.Eq("WalletID", walletID),
	)

	if offset > 0 {
		query = query.Skip(int(offset))
	}

	if limit > 0 {
		query = query.Limit(int(limit))
	}

	if newestFirst {
		query = query.OrderBy("StartTime").Reverse()
	} else {
		query = query.OrderBy("StartTime")
	}

	var agendas []Agenda
	err := query.Find(&agendas)
	if err != nil && err != storm.ErrNotFound {
		return nil, fmt.Errorf("error fetching agendas: %s", err.Error())
	}

	return agendas, nil
}

// GetAgendasByWalletID returns the result of GetAgendasByWalletIDRaw as a JSON string
func (c *Consensus) GetAgendasByWalletID(walletID int, offset, limit int32, newestFirst bool) (string, error) {

	result, err := c.GetAgendasByWalletIDRaw(walletID, offset, limit, newestFirst)
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

// GetAgendaRaw fetches and returns a single agenda specified by it's #AgendaID
func (c *Consensus) GetAgendaRaw(agendaID string) (*Agenda, error) {
	var agenda Agenda
	err := c.mwRef.db.One("AgendaID", agendaID, &agenda)
	if err != nil {
		return nil, err
	}

	return &agenda, nil
}

// GetAgenda returns the result of GetAgendaRaw as a JSON string
func (c *Consensus) GetAgenda(agendaID string) (string, error) {
	return c.marshalResult(c.GetAgendaRaw(agendaID))
}

// GetProposalByIDRaw fetches and returns a single proposal specified by it's ID
func (c *Consensus) GetAgendaByIDRaw(ID int) (*Agenda, error) {
	var agenda Agenda
	err := c.mwRef.db.One("ID", ID, &agenda)
	if err != nil {
		return nil, err
	}

	return &agenda, nil
}

// GetProposalByID returns the result of GetProposalByIDRaw as a JSON string
func (c *Consensus) GetAgendaByID(ID int) (string, error) {
	return c.marshalResult(c.GetAgendaByIDRaw(ID))
}

func (c *Consensus) ClearSavedAgendas() error {
	err := c.mwRef.db.Drop(&Agenda{})
	if err != nil {
		return translateError(err)
	}

	return c.mwRef.db.Init(&Agenda{})
}

func (c *Consensus) ClearSavedVoteChoices() error {
	err := c.mwRef.db.Drop(&VoteChoice{})
	if err != nil {
		return translateError(err)
	}

	return c.mwRef.db.Init(&VoteChoice{})
}

func (c *Consensus) marshalResult(result interface{}, err error) (string, error) {

	if err != nil {
		return "", translateError(err)
	}

	response, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("error marshalling result: %s", err.Error())
	}

	return string(response), nil
}
