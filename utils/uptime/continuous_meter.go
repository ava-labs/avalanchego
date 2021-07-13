// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	running bool

	halflife    float64
	value       float64
	lastUpdated time.Time
}

// NewMeter returns a new Meter with the provided halflife
func NewMeter(halflife time.Duration) Meter {
	return &continuousMeter{
		halflife: float64(halflife) / convertEToBase2,
	}
}

func (a *continuousMeter) Start(currentTime time.Time) {
	a.update(currentTime, true)
}

func (a *continuousMeter) Stop(currentTime time.Time) {
	a.update(currentTime, false)
}

func (a *continuousMeter) update(currentTime time.Time, running bool) {
	if a.running == running {
		return
	}
	a.Read(currentTime)
	a.running = running
}

func (a *continuousMeter) Read(currentTime time.Time) float64 {
	timeSincePreviousUpdate := a.lastUpdated.Sub(currentTime)
	if timeSincePreviousUpdate >= 0 {
		return a.value
	}
	a.lastUpdated = currentTime

	factor := math.Exp(float64(timeSincePreviousUpdate) / a.halflife)
	a.value *= factor
	if a.running {
		a.value += 1 - factor
	}
	return a.value
}
