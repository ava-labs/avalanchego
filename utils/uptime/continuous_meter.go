// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"math"
	"time"
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

	halflife    time.Duration
	value       float64
	lastUpdated time.Time
}

// NewMeter returns a new Meter with the provided halflife
func NewMeter(halflife time.Duration) Meter {
	return &continuousMeter{halflife: halflife}
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
	timeSincePreviousUpdate := currentTime.Sub(a.lastUpdated)
	if timeSincePreviousUpdate <= 0 {
		return a.value
	}
	a.lastUpdated = currentTime

	factor := math.Pow(2, -float64(timeSincePreviousUpdate)/float64(a.halflife))
	a.value *= factor
	if a.running {
		a.value += 1 - factor
	}
	return a.value
}
