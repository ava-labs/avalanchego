// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"math"
	"time"
)

var convertEToBase2 = math.Log(2)

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
		halflife:    float64(halflife) / convertEToBase2,
		weightedSum: initialPrediction,
		normalizer:  1,
		lastUpdated: currentTime,
	}
}

func (a *continuousAverager) Observe(value float64, currentTime time.Time) {
	delta := a.lastUpdated.Sub(currentTime)
	switch {
	case delta < 0:
		// If the times are called in order, scale the previous values to keep the
		// sizes manageable
		newWeight := math.Exp(float64(delta) / a.halflife)

		a.weightedSum = value + newWeight*a.weightedSum
		a.normalizer = 1 + newWeight*a.normalizer

		a.lastUpdated = currentTime
	case delta == 0:
		// If this is called multiple times at the same wall clock time, no
		// scaling needs to occur
		a.weightedSum += value
		a.normalizer++
	default:
		// If the times are called out of order, don't scale the previous values
		newWeight := math.Exp(float64(-delta) / a.halflife)

		a.weightedSum += newWeight * value
		a.normalizer += newWeight
	}
}

func (a *continuousAverager) Read() float64 {
	return a.weightedSum / a.normalizer
}
