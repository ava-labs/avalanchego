// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import "time"

// maturedAverager wraps an Averager and returns zero until the averager has
// been running for at least the half-life duration.
type maturedAverager struct {
	Averager
	halflife       time.Duration
	startTime      time.Time
	lastObserved   time.Time
	hasObserved    bool
}

// NewMaturedAverager creates a new matured averager that wraps the provided
// averager. It returns zero from Read() until at least halflife duration has
// passed since the first Observe call.
func NewMaturedAverager(halflife time.Duration, averager Averager) Averager {
	return &maturedAverager{
		Averager: averager,
		halflife:   halflife,
	}
}

func (a *maturedAverager) Observe(value float64, currentTime time.Time) {
	if !a.hasObserved {
		a.startTime = currentTime
		a.hasObserved = true
	}
	a.lastObserved = currentTime
	a.Averager.Observe(value, currentTime)
}

func (a *maturedAverager) Read() float64 {
	// If we haven't observed anything yet, return zero
	if !a.hasObserved {
		return 0
	}

	// Check if enough time has passed since the first observation
	elapsed := a.lastObserved.Sub(a.startTime)
	if elapsed < a.halflife {
		return 0
	}

	return a.Averager.Read()
}
