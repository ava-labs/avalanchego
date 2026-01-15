// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meter

import (
	"math"
	"time"
)

var (
	_ Factory = (*ContinuousFactory)(nil)
	_ Meter   = (*continuousMeter)(nil)
)

// ContinuousFactory implements the Factory interface by returning a continuous
// time meter.
type ContinuousFactory struct{}

func (ContinuousFactory) New(halflife time.Duration) Meter {
	return NewMeter(halflife)
}

type continuousMeter struct {
	halflife float64
	value    float64

	numCoresRunning float64
	lastUpdated     time.Time
}

// NewMeter returns a new Meter with the provided halflife
func NewMeter(halflife time.Duration) Meter {
	return &continuousMeter{
		halflife: float64(halflife) / math.Ln2,
	}
}

func (a *continuousMeter) Inc(now time.Time, numCores float64) {
	a.Read(now)
	a.numCoresRunning += numCores
}

func (a *continuousMeter) Dec(now time.Time, numCores float64) {
	a.Read(now)
	a.numCoresRunning -= numCores
}

func (a *continuousMeter) Read(now time.Time) float64 {
	timeSincePreviousUpdate := a.lastUpdated.Sub(now)
	if timeSincePreviousUpdate >= 0 {
		return a.value
	}
	a.lastUpdated = now

	factor := math.Exp(float64(timeSincePreviousUpdate) / a.halflife)
	a.value *= factor
	a.value += a.numCoresRunning * (1 - factor)
	return a.value
}

func (a *continuousMeter) TimeUntil(now time.Time, value float64) time.Duration {
	currentValue := a.Read(now)
	if currentValue <= value {
		return time.Duration(0)
	}
	// Note that [factor] >= 1
	factor := currentValue / value
	// Note that [numHalfLives] >= 0
	numHalflives := math.Log(factor)
	duration := numHalflives * a.halflife
	// Overflow protection
	if duration > math.MaxInt64 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(duration)
}
