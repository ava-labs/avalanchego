// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"math"
	"time"
)

type continuousAverager struct {
	halflife    float64
	weightedSum float64
	normalizer  float64
	lastUpdated time.Time
}

func NewAverager(
	initialPrediction float64,
	halflife time.Duration,
	currentTime time.Time,
) Averager {
	return &continuousAverager{
		halflife:    float64(halflife) / math.Log(2),
		weightedSum: initialPrediction,
		normalizer:  1,
		lastUpdated: currentTime,
	}
}

func (a *continuousAverager) Observe(value float64, currentTime time.Time) {
	previousTime := a.lastUpdated
	if a.lastUpdated.Before(currentTime) {
		a.lastUpdated = currentTime
	}

	// negative if this call is out of order, otherwise zero
	newDelta := currentTime.Sub(a.lastUpdated)
	// zero if this call is out of order, otherwise negative
	oldDelta := previousTime.Sub(a.lastUpdated)

	newWeightedDelta := math.Exp(float64(newDelta) / a.halflife)
	oldWeightedDelta := math.Exp(float64(oldDelta) / a.halflife)

	a.weightedSum = newWeightedDelta*value + oldWeightedDelta*a.weightedSum
	a.normalizer = newWeightedDelta + oldWeightedDelta*a.normalizer
}

func (a *continuousAverager) Read() float64 {
	return a.weightedSum / a.normalizer
}
