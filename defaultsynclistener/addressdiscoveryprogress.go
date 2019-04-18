package defaultsynclistener

import (
	"fmt"
	"math"
	"time"

	"github.com/raedahgroup/dcrlibwallet"
)

func (syncListener *defaultSyncListener) OnDiscoveredAddresses(state string) {
	if state == dcrlibwallet.SyncStateStart && syncListener.addressDiscoveryCompleted == nil {
		if syncListener.showLog {
			fmt.Println("Step 2 of 3 - discovering used addresses.")
		}
		syncListener.updateAddressDiscoveryProgress()
	} else {
		close(syncListener.addressDiscoveryCompleted)
		syncListener.addressDiscoveryCompleted = nil
	}
}

func (syncListener *defaultSyncListener) updateAddressDiscoveryProgress() {
	// update progress report with info of current step
	syncListener.progressReport.Update(SyncStatusInProgress, func(report *progressReport) {
		report.CurrentStep = DiscoveringUsedAddresses
	})

	// these values will be used every second to calculate the total sync progress
	addressDiscoveryStartTime := time.Now().Unix()
	totalHeadersFetchTime := float64(syncListener.headersFetchTimeSpent)
	estimatedRescanTime := totalHeadersFetchTime * RescanPercentage
	estimatedDiscoveryTime := totalHeadersFetchTime * DiscoveryPercentage

	// following channels are used to determine next step in the below subroutine
	everySecondTicker := time.NewTicker(1 * time.Second)
	everySecondTickerChannel := everySecondTicker.C

	// track last logged time remaining and total percent to avoid re-logging same message
	var lastTimeRemaining string
	var lastTotalPercent int32 = -1

	syncListener.addressDiscoveryCompleted = make(chan bool)

	go func() {
		for {
			select {
			case <-everySecondTickerChannel:
				// calculate address discovery progress
				elapsedDiscoveryTime := float64(time.Now().Unix() - addressDiscoveryStartTime)
				discoveryProgress := (elapsedDiscoveryTime / estimatedDiscoveryTime) * 100

				var totalSyncTime float64
				if elapsedDiscoveryTime > estimatedDiscoveryTime {
					totalSyncTime = totalHeadersFetchTime + elapsedDiscoveryTime + estimatedRescanTime
				} else {
					totalSyncTime = totalHeadersFetchTime + estimatedDiscoveryTime + estimatedRescanTime
				}

				totalElapsedTime := totalHeadersFetchTime + elapsedDiscoveryTime
				totalProgress := (totalElapsedTime / totalSyncTime) * 100

				remainingAccountDiscoveryTime := math.Round(estimatedDiscoveryTime - elapsedDiscoveryTime)
				if remainingAccountDiscoveryTime < 0 {
					remainingAccountDiscoveryTime = 0
				}

				totalProgressPercent := int32(math.Round(totalProgress))
				totalTimeRemaining := calculateTotalTimeRemaining(remainingAccountDiscoveryTime + estimatedRescanTime)

				// update address discovery progress, total progress and total time remaining
				syncListener.progressReport.Update(SyncStatusInProgress, func(report *progressReport) {
					report.AddressDiscoveryProgress = int32(math.Round(discoveryProgress))
					report.TotalSyncProgress = totalProgressPercent
					report.TotalTimeRemaining = totalTimeRemaining
				})

				if syncListener.showLog {
					// avoid logging same message multiple times
					if totalProgressPercent != lastTotalPercent || totalTimeRemaining != lastTimeRemaining {
						fmt.Printf("Syncing %d%%, %s remaining, discovering used addresses.\n",
							totalProgressPercent, totalTimeRemaining)

						lastTotalPercent = totalProgressPercent
						lastTimeRemaining = totalTimeRemaining
					}
				}

			case <-syncListener.addressDiscoveryCompleted:
				// stop updating time taken and progress for address discovery
				everySecondTicker.Stop()

				// update final discovery time taken
				addressDiscoveryFinishTime := time.Now().Unix()
				syncListener.totalDiscoveryTimeSpent = addressDiscoveryFinishTime - addressDiscoveryStartTime

				if syncListener.showLog {
					fmt.Println("Address discovery complete.")
				}

				return
			}
		}
	}()
}
