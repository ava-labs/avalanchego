// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"math"
	"time"

	"github.com/ava-labs/gecko/utils/timer"
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

	clock timer.Clock
}

// NewMeter returns a new Meter with the provided halflife
func NewMeter(halflife time.Duration) Meter {
	return &meter{halflife: halflife}
}

func (a *meter) Start() {
	if a.running {
		return
	}
	a.Read()
	a.running = true
}

func (a *meter) Stop() {
	if !a.running {
		return
	}
	a.Read()
	a.running = false
}

func (a *meter) Read() float64 {
	currentTime := a.clock.Time()
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
