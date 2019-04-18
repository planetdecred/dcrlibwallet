package defaultsynclistener

import "fmt"

func (syncListener *DefaultSyncListener) OnPeerConnected(peerCount int32) {
	syncListener.handlePeerCountUpdate(peerCount)
}

func (syncListener *DefaultSyncListener) OnPeerDisconnected(peerCount int32) {
	syncListener.handlePeerCountUpdate(peerCount)
}

func (syncListener *DefaultSyncListener) handlePeerCountUpdate(peerCount int32) {
	// update peer count but maintain current sync status
	currentSyncStatus := syncListener.progressReport.Read().Status
	syncListener.progressReport.Update(currentSyncStatus, func(report *progressReport) {
		report.ConnectedPeers = peerCount
	})

	syncListener.syncProgressUpdated(syncListener.progressReport, PeersCountUpdate)

	if syncListener.showLog {
		if peerCount == 1 {
			fmt.Printf("Connected to %d peer on %s.\n", peerCount, syncListener.netType)
		} else {
			fmt.Printf("Connected to %d peers on %s.\n", peerCount, syncListener.netType)
		}
	}
}
