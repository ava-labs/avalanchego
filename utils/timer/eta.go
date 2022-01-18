// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"time"
)

// EstimateETA attempts to estimate the remaining time for a job to finish given
// the [startTime] and it's current progress.
func EstimateETA(startTime time.Time, progress, end uint64) time.Duration {
	timeSpent := time.Since(startTime)

	percentExecuted := float64(progress) / float64(end)
	estimatedTotalDuration := time.Duration(float64(timeSpent) / percentExecuted)
	return estimatedTotalDuration - timeSpent
}
