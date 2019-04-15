package dcrlibwallet

import (
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/spv"
	"github.com/decred/dcrwallet/wallet"
	"github.com/raedahgroup/dcrlibwallet/blockchainsync"
)

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
			// begin indexing transactions after blockchainsync is completed,
			// syncProgressListeners.OnSynced() will be invoked after transactions are indexed
			lw.IndexTransactions(-1, -1, func() {
				for _, syncResponse := range lw.syncProgressListeners {
					syncResponse.OnSynced(sync)
				}
			})
		},
		FetchMissingCFiltersStarted: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchMissingCFilters(0, 0, blockchainsync.START)
			}
		},
		FetchMissingCFiltersProgress: func(missingCFitlersStart, missingCFitlersEnd int32) {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchMissingCFilters(missingCFitlersStart, missingCFitlersEnd, blockchainsync.PROGRESS)
			}
		},
		FetchMissingCFiltersFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchMissingCFilters(0, 0, blockchainsync.FINISH)
			}
		},
		FetchHeadersStarted: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchedHeaders(0, 0, blockchainsync.START)
			}
		},
		FetchHeadersProgress: func(fetchedHeadersCount int32, lastHeaderTime int64) {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchedHeaders(fetchedHeadersCount, lastHeaderTime, blockchainsync.PROGRESS)
			}
		},
		FetchHeadersFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchedHeaders(0, 0, blockchainsync.FINISH)
			}
		},
		DiscoverAddressesStarted: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnDiscoveredAddresses(blockchainsync.START)
			}
		},
		DiscoverAddressesFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnDiscoveredAddresses(blockchainsync.FINISH)
			}

			if !loadedWallet.Locked() {
				loadedWallet.Lock()
			}
		},
		RescanStarted: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(0, blockchainsync.START)
			}
		},
		RescanProgress: func(rescannedThrough int32) {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(rescannedThrough, blockchainsync.PROGRESS)
			}
		},
		RescanFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(0, blockchainsync.FINISH)
			}
		},
	}
}

func (lw *LibWallet) notifySyncError(code blockchainsync.ErrorCode, err error) {
	for _, syncResponse := range lw.syncProgressListeners {
		syncResponse.OnSyncError(code, err)
	}
}
