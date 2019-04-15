package blockchainsync

import (
	"fmt"
	"math"
	"time"
)

func calculateTotalTimeRemaining(timeRemainingInSeconds float64) string {
	minutes := int64(math.Round(timeRemainingInSeconds / 60))
	if minutes > 0 {
		return fmt.Sprintf("%d min", minutes)
	}
	return fmt.Sprintf("%d sec", int64(math.Round(timeRemainingInSeconds)))
}

func calculateDaysBehind(lastHeaderTime int64) string {
	hoursBehind := float64(time.Now().Unix()-lastHeaderTime) / 60
	daysBehind := int(math.Round(hoursBehind / 24))
	if daysBehind < 1 {
		return "<1 day"
	} else if daysBehind == 1 {
		return "1 day"
	} else {
		return fmt.Sprintf("%d days", daysBehind)
	}
}

func estimateFinalBlockHeight(netType string, bestBlockTimeStamp int64, bestBlock int32) int32 {
	var targetTimePerBlock int32
	if netType == "mainnet" {
		targetTimePerBlock = MainNetTargetTimePerBlock
	} else {
		targetTimePerBlock = TestNetTargetTimePerBlock
	}

	timeDifference := time.Now().Unix() - bestBlockTimeStamp
	return (int32(timeDifference) / targetTimePerBlock) + bestBlock
}
