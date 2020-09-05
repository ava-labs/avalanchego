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
	Start(time.Time)

	// Stop the meter, the read value will be exponentially decreasing while the
	// meter is off.
	Stop(time.Time)

	// Read the current value of the meter, this can be used to approximate the
	// percent of time the meter has been running recently. The definition of
	// recently depends on the halflife of the decay function.
	Read(time.Time) float64
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
	return &meter{halflife: halflife}
}

// Start implements the Meter interface
func (a *meter) Start(currentTime time.Time) {
	if a.running {
		return
	}
	a.Read(currentTime)
	a.running = true
}

// Stop implements the Meter interface
func (a *meter) Stop(currentTime time.Time) {
	if !a.running {
		return
	}
	a.Read(currentTime)
	a.running = false
}

// Stop implements the Meter interface
func (a *meter) Read(currentTime time.Time) float64 {
	timeSincePreviousUpdate := currentTime.Sub(a.lastUpdated)
	if timeSincePreviousUpdate <= 0 {
		return a.value
	}
	a.lastUpdated = currentTime

	factor := math.Pow(2, -timeSincePreviousUpdate.Seconds()/a.halflife.Seconds())
	a.value *= factor
	if a.running {
		a.value += 1 - factor
	}
	return a.value
}
