package dcrlibwallet

import (
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/spv"
	"github.com/decred/dcrwallet/wallet"
)

const (
	// Sync States
	START    = "start"
	FINISH   = "finish"
	PROGRESS = "progress"
)

type SyncProgressListener interface {
	OnPeerConnected(peerCount int32)
	OnPeerDisconnected(peerCount int32)
	OnFetchMissingCFilters(missingCFiltersStart, missingCFiltersEnd int32, state string)
	OnFetchedHeaders(fetchedHeadersCount int32, lastHeaderTime int64, state string)
	OnDiscoveredAddresses(state string)
	OnRescan(rescannedThrough int32, state string)
	OnIndexTransactions(totalIndex int32)
	OnSynced(synced bool)
	/*
	* Handled Error Codes
	* -1 - Unexpected Error
	*  1 - Context Canceled
	*  2 - Deadline Exceeded
	*  3 - Invalid Address
	 */
	OnSyncError(code int, err error)
}

func (lw *LibWallet) spvSyncNotificationCallbacks(loadedWallet *wallet.Wallet) *spv.Notifications {
	generalNotifications := lw.generalSyncNotificationCallbacks(loadedWallet)
	return &spv.Notifications{
		Synced:                       generalNotifications.Synced,
		FetchHeadersStarted:          generalNotifications.FetchHeadersStarted,
		FetchHeadersProgress:         generalNotifications.FetchHeadersProgress,
		FetchHeadersFinished:         generalNotifications.FetchHeadersFinished,
		FetchMissingCFiltersStarted:  generalNotifications.FetchMissingCFiltersStarted,
		FetchMissingCFiltersProgress: generalNotifications.FetchMissingCFiltersProgress,
		FetchMissingCFiltersFinished: generalNotifications.FetchMissingCFiltersFinished,
		DiscoverAddressesStarted:     generalNotifications.DiscoverAddressesStarted,
		DiscoverAddressesFinished:    generalNotifications.DiscoverAddressesFinished,
		RescanStarted:                generalNotifications.RescanStarted,
		RescanProgress:               generalNotifications.RescanProgress,
		RescanFinished:               generalNotifications.RescanFinished,
		PeerDisconnected: func(peerCount int32, addr string) {
			for _, syncResponse := range lw.syncProgressListeners {
				syncResponse.OnPeerDisconnected(peerCount)
			}
		},
		PeerConnected: func(peerCount int32, addr string) {
			for _, syncResponse := range lw.syncProgressListeners {
				syncResponse.OnPeerConnected(peerCount)
			}
		},
	}
}

func (lw *LibWallet) generalSyncNotificationCallbacks(loadedWallet *wallet.Wallet) *chain.Notifications {
	return &chain.Notifications{
		Synced: func(sync bool) {
			// begin indexing transactions after sync is completed,
			// syncProgressListeners.OnSynced() will be invoked after transactions are indexed
			lw.IndexTransactions(-1, -1, func() {
				for _, syncResponse := range lw.syncProgressListeners {
					syncResponse.OnSynced(sync)
				}
			})
		},
		FetchMissingCFiltersStarted: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchMissingCFilters(0, 0, START)
			}
		},
		FetchMissingCFiltersProgress: func(missingCFitlersStart, missingCFitlersEnd int32) {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchMissingCFilters(missingCFitlersStart, missingCFitlersEnd, PROGRESS)
			}
		},
		FetchMissingCFiltersFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchMissingCFilters(0, 0, FINISH)
			}
		},
		FetchHeadersStarted: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchedHeaders(0, 0, START)
			}
		},
		FetchHeadersProgress: func(fetchedHeadersCount int32, lastHeaderTime int64) {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchedHeaders(fetchedHeadersCount, lastHeaderTime, PROGRESS)
			}
		},
		FetchHeadersFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchedHeaders(0, 0, FINISH)
			}
		},
		DiscoverAddressesStarted: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnDiscoveredAddresses(START)
			}
		},
		DiscoverAddressesFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnDiscoveredAddresses(FINISH)
			}

			if !loadedWallet.Locked() {
				loadedWallet.Lock()
			}
		},
		RescanStarted: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(0, START)
			}
		},
		RescanProgress: func(rescannedThrough int32) {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(rescannedThrough, PROGRESS)
			}
		},
		RescanFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(0, FINISH)
			}
		},
	}
}

func (lw *LibWallet) notifySyncError(code int, err error) {
	for _, syncResponse := range lw.syncProgressListeners {
		syncResponse.OnSyncError(code, err)
	}
}
