package dcrlibwallet

import (
	"time"

	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/spv"
	"github.com/decred/dcrwallet/wallet"
)

type SyncProgressListener interface {
	OnPeerConnected(peerCount int32)
	OnPeerDisconnected(peerCount int32)
	OnFetchMissingCFilters(missingCFiltersStart, missingCFiltersEnd int32, state string)
	OnFetchedHeaders(beginFetchTimeStamp, headersFetchTimeSpent, lastHeaderTime int64, fetchedHeadersCount, startHeaderHeight, totalFetchedHeaders int32, state string)
	OnDiscoveredAddresses(headersFetchTimeSpent, addressDiscoveryStartTime int64, state string)
	OnRescan(rescannedThrough int32, rescanStartTime, headersFetchTimeSpent, totalDiscoveryTimeSpent int64, state string)
	OnIndexTransactions(totalIndex int32)
	OnSynced(synced bool)
	OnSyncEndedWithError(code int32, err error) // use int32 to allow gomobile bind
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

			lw.beginFetchTimeStamp = -1
			lw.headersFetchTimeSpent = -1
			lw.totalDiscoveryTimeSpent = -1

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
				syncProgressListener.OnFetchMissingCFilters(0, 0, SyncStateStart)
			}
		},
		FetchMissingCFiltersProgress: func(missingCFitlersStart, missingCFitlersEnd int32) {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchMissingCFilters(missingCFitlersStart, missingCFitlersEnd, SyncStateProgress)
			}
		},
		FetchMissingCFiltersFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchMissingCFilters(0, 0, SyncStateFinish)
			}
		},
		FetchHeadersStarted: func() {
			if lw.beginFetchTimeStamp != -1 {
				// already started headers fetching
				return
			}

			lw.beginFetchTimeStamp = time.Now().Unix()
			lw.startHeaderHeight = lw.GetBestBlock()
			lw.currentHeaderHeight = lw.startHeaderHeight

			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchedHeaders(lw.beginFetchTimeStamp, lw.headersFetchTimeSpent, 0, 0,
					lw.startHeaderHeight, lw.currentHeaderHeight, SyncStateStart)
			}
		},
		FetchHeadersProgress: func(fetchedHeadersCount int32, lastHeaderTime int64) {

			lw.currentHeaderHeight += fetchedHeadersCount

			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchedHeaders(lw.beginFetchTimeStamp, lw.headersFetchTimeSpent, lastHeaderTime,
					fetchedHeadersCount, lw.startHeaderHeight, lw.currentHeaderHeight, SyncStateProgress)
			}
		},
		FetchHeadersFinished: func() {

			lw.headersFetchTimeSpent = time.Now().Unix() - lw.beginFetchTimeStamp
			lw.startHeaderHeight = -1
			lw.currentHeaderHeight = -1

			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnFetchedHeaders(lw.beginFetchTimeStamp, lw.headersFetchTimeSpent, 0, 0, lw.startHeaderHeight, lw.currentHeaderHeight, SyncStateFinish)
			}
		},
		DiscoverAddressesStarted: func() {

			lw.addressDiscoveryStartTime = time.Now().Unix()

			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnDiscoveredAddresses(lw.headersFetchTimeSpent, lw.addressDiscoveryStartTime, SyncStateStart)
			}
		},
		DiscoverAddressesFinished: func() {

			addressDiscoveryFinishTime := time.Now().Unix()
			lw.totalDiscoveryTimeSpent = addressDiscoveryFinishTime - lw.addressDiscoveryStartTime

			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnDiscoveredAddresses(lw.headersFetchTimeSpent, lw.addressDiscoveryStartTime, SyncStateFinish)
			}

			if !loadedWallet.Locked() {
				loadedWallet.Lock()
			}
		},
		RescanStarted: func() {

			lw.rescanStartTime = time.Now().Unix()
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(0, lw.rescanStartTime, lw.headersFetchTimeSpent, lw.totalDiscoveryTimeSpent, SyncStateStart)
			}
		},
		RescanProgress: func(rescannedThrough int32) {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(rescannedThrough, lw.rescanStartTime, lw.headersFetchTimeSpent, lw.totalDiscoveryTimeSpent, SyncStateProgress)
			}
		},
		RescanFinished: func() {
			for _, syncProgressListener := range lw.syncProgressListeners {
				syncProgressListener.OnRescan(0, lw.rescanStartTime, lw.headersFetchTimeSpent, lw.totalDiscoveryTimeSpent, SyncStateFinish)
			}
		},
	}
}

func (lw *LibWallet) notifySyncError(code SyncErrorCode, err error) {
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnSyncEndedWithError(int32(code), err)
	}
}

func (lw *LibWallet) notifySyncCanceled() {
	for _, syncProgressListener := range lw.syncProgressListeners {
		syncProgressListener.OnSynced(false)
	}
}
