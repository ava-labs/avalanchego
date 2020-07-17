// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"math"
	"time"
)

// Meter tracks a continuous exponential moving average of the % of time this
// meter has been running.
type Meter interface {
	// Start the meter, the read value will be monotonically increasing while
	// the meter is running.
	Start()

	// Stop the meter, the read value will be exponentially decreasing while the
	// meter is off.
	Stop()

	// Read the current value of the meter, this can be used to approximate the
	// percent of time the meter has been running recently. The definition of
	// recently depends on the halflife of the decay function.
	Read() float64
}

type meter struct {
	running bool
	started time.Time

	halflife time.Duration

	value       float64
	lastUpdated time.Time
}

// NewMeter returns a new Meter with the provided halflife
func NewMeter(halflife time.Duration) Meter {
	return &meter{
		running: false,
		started: time.Time{},

		halflife:    halflife,
		value:       0,
		lastUpdated: time.Now(),
	}
}

func (a *meter) Start() {
	if a.running {
		return
	}
	a.running = true

	t := time.Now()
	a.offUntil(t)
}

func (a *meter) Stop() {
	if !a.running {
		return
	}
	a.running = false

	t := time.Now()
	a.onUntil(t)
}

func (a *meter) Read() float64 {
	t := time.Now()
	if a.running {
		a.onUntil(t)
	} else {
		a.offUntil(t)
	}
	return a.value
}

func (a *meter) offUntil(t time.Time) {
	timeOff := t.Sub(a.lastUpdated)
	if timeOff < 0 {
		// This should never happen
		return
	}
	factor := math.Pow(2, -timeOff.Seconds()/a.halflife.Seconds())
	a.value = a.value * factor
	a.lastUpdated = t
}

func (a *meter) onUntil(t time.Time) {
	timeOn := t.Sub(a.lastUpdated)
	if timeOn < 0 {
		// This should never happen
		return
	}
	factor := math.Pow(2, -timeOn.Seconds()/a.halflife.Seconds())
	a.value = a.value*factor + 1 - factor
	a.lastUpdated = t
}
