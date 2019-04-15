package blockchainsync

import (
	"fmt"
	"math"
	"time"
)

const (
	// Approximate time (in seconds) to mine a block in mainnet
	MainNetTargetTimePerBlock = 300

	// Approximate time (in seconds) to mine a block in testnet
	TestNetTargetTimePerBlock = 120

	// Use 10% of estimated total headers fetch time to estimate rescan time
	RescanPercentage = 0.1

	// Use 80% of estimated total headers fetch time to estimate address discovery time
	DiscoveryPercentage = 0.8
)

func updateFetchHeadersProgress(syncInfo *info, fetchHeadersData *FetchHeadersData, report FetchHeadersProgressReport) {
	// increment current block height value
	fetchHeadersData.CurrentHeaderHeight += report.FetchedHeadersCount

	// calculate percentage progress and eta
	totalFetchedHeaders := fetchHeadersData.CurrentHeaderHeight
	if fetchHeadersData.StartHeaderHeight > 0 {
		totalFetchedHeaders -= fetchHeadersData.StartHeaderHeight
	}

	syncEndPoint := report.EstimatedFinalBlockHeight - fetchHeadersData.StartHeaderHeight
	headersFetchingRate := float64(totalFetchedHeaders) / float64(syncEndPoint)

	timeTakenSoFar := time.Now().Unix() - fetchHeadersData.BeginFetchTimeStamp
	estimatedTotalHeadersFetchTime := math.Round(float64(timeTakenSoFar) / headersFetchingRate)

	estimatedRescanTime := math.Round(estimatedTotalHeadersFetchTime * RescanPercentage)
	estimatedDiscoveryTime := math.Round(estimatedTotalHeadersFetchTime * DiscoveryPercentage)
	estimatedTotalSyncTime := estimatedTotalHeadersFetchTime + estimatedRescanTime + estimatedDiscoveryTime

	totalTimeRemaining := estimatedTotalSyncTime - float64(timeTakenSoFar)
	totalSyncProgress := (float64(timeTakenSoFar) / float64(estimatedTotalSyncTime)) * 100.0

	// update total progress info
	syncInfo.TotalSyncProgress = int32(math.Round(totalSyncProgress))
	syncInfo.TotalTimeRemaining = calculateTotalTimeRemaining(totalTimeRemaining)

	// update headers statistics
	syncInfo.TotalHeadersToFetch = syncEndPoint
	syncInfo.DaysBehind = calculateDaysBehind(report.LastHeaderTime)

	// update headers progress info
	syncInfo.FetchedHeadersCount = totalFetchedHeaders
	syncInfo.HeadersFetchProgress = int32(math.Round(headersFetchingRate * 100))
}

func updateAddressDiscoveryProgress(privateSyncInfo *PrivateSyncInfo, showLog bool, syncInfoUpdated func(*PrivateSyncInfo)) chan bool {
	updateSyncInfo := func(update func(*info)) {
		syncInfo := privateSyncInfo.Read()
		update(syncInfo)
		privateSyncInfo.Write(syncInfo, StatusInProgress)
		syncInfoUpdated(privateSyncInfo)
	}

	// update sync info current step
	updateSyncInfo(func(syncInfo *info) {
		syncInfo.CurrentStep = 2
	})

	// these values will be used every second to calculate the total sync progress
	addressDiscoveryStartTime := time.Now().Unix()
	totalHeadersFetchTime := float64(privateSyncInfo.Read().HeadersFetchTimeTaken)
	estimatedRescanTime := totalHeadersFetchTime * RescanPercentage
	estimatedDiscoveryTime := totalHeadersFetchTime * DiscoveryPercentage

	// following channels are used to determine next step in the below subroutine
	everySecondTicker := time.NewTicker(1 * time.Second)
	everySecondTickerChannel := everySecondTicker.C
	discoveryCompletedChannel := make(chan bool)

	// track last logged time remaining and total percent to avoid re-logging same message
	var lastTimeRemaining string
	var lastTotalPercent int32 = -1

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
				totalTimeRemaining := remainingAccountDiscoveryTime + estimatedRescanTime

				// update address discovery progress, total progress and total time remaining
				updateSyncInfo(func(syncInfo *info) {
					syncInfo.AddressDiscoveryProgress = int32(math.Round(discoveryProgress))
					syncInfo.TotalSyncProgress = int32(math.Round(totalProgress))
					syncInfo.TotalTimeRemaining = calculateTotalTimeRemaining(totalTimeRemaining)

					if showLog && !syncInfo.Done {
						if syncInfo.TotalSyncProgress != lastTotalPercent || syncInfo.TotalTimeRemaining != lastTimeRemaining {
							fmt.Printf("Syncing %d%%, %s remaining, discovering used addresses.\n",
								syncInfo.TotalSyncProgress, syncInfo.TotalTimeRemaining)

							lastTotalPercent = syncInfo.TotalSyncProgress
							lastTimeRemaining = syncInfo.TotalTimeRemaining
						}
					}
				})

			case <-discoveryCompletedChannel:
				// stop updating time taken and progress for address discovery
				everySecondTicker.Stop()

				// update final discovery time taken
				addressDiscoveryFinishTime := time.Now().Unix()
				updateSyncInfo(func(syncInfo *info) {
					syncInfo.TotalDiscoveryTime = addressDiscoveryFinishTime - addressDiscoveryStartTime

					if showLog && !syncInfo.Done {
						fmt.Println("Address discovery complete.")
					}
				})

				return
			}
		}
	}()

	return discoveryCompletedChannel
}
