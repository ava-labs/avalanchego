// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"math"
	"time"
)

var (
	convertEToBase2         = math.Log(2)
	_               Meter   = &continuousMeter{}
	_               Factory = &ContinuousFactory{}
)

// ContinuousFactory implements the Factory interface by returning a continuous
// time meter.
type ContinuousFactory struct{}

// New implements the Factory interface.
func (ContinuousFactory) New(halflife time.Duration) Meter {
	return NewMeter(halflife)
}

type continuousMeter struct {
	halflife float64
	value    float64

	numCoresRunning uint
	lastUpdated     time.Time
}

// NewMeter returns a new Meter with the provided halflife
func NewMeter(halflife time.Duration) Meter {
	return &continuousMeter{
		halflife: float64(halflife) / convertEToBase2,
	}
}

func (a *continuousMeter) Start(currentTime time.Time) {
	a.Read(currentTime)
	a.numCoresRunning++
}

func (a *continuousMeter) Stop(currentTime time.Time) {
	a.Read(currentTime)
	a.numCoresRunning--
}

func (a *continuousMeter) Read(currentTime time.Time) float64 {
	timeSincePreviousUpdate := a.lastUpdated.Sub(currentTime)
	if timeSincePreviousUpdate >= 0 {
		return a.value
	}
	a.lastUpdated = currentTime

	factor := math.Exp(float64(timeSincePreviousUpdate) / a.halflife)
	a.value *= factor
	a.value += float64(a.numCoresRunning) * (1 - factor)
	return a.value
}
