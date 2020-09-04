// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/gecko/utils/timer"
)

const (
	maxSkippedIntervals = 5
)

type intervalMeter struct {
	running bool
	started time.Time

	halflife time.Duration

	value         float64
	nextHalvening time.Time
	lastUpdated   time.Time

	clock timer.Clock
}

// NewIntervalMeter returns a new Meter with the provided halflife
func NewIntervalMeter(halflife time.Duration) Meter {
	m := &intervalMeter{halflife: halflife}
	m.lastUpdated = m.clock.Time()
	m.nextHalvening = m.lastUpdated.Add(halflife)
	return m
}

func (a *intervalMeter) Start() {
	if a.running {
		return
	}
	a.Read()
	a.running = true
}

func (a *intervalMeter) Stop() {
	if !a.running {
		return
	}
	a.Read()
	a.running = false
}

func (a *intervalMeter) Read() float64 {
	currentTime := a.clock.Time()
	if !currentTime.After(a.lastUpdated) {
		return a.value
	}
	if totalTime := currentTime.Sub(a.lastUpdated); totalTime > maxSkippedIntervals*a.halflife {
		if a.running {
			a.value = 1
		} else {
			a.value = 0
		}
	} else {
		for !currentTime.Before(a.nextHalvening) {
			if a.running {
				additionalRunningTime := float64(a.nextHalvening.Sub(a.lastUpdated)) / float64(a.halflife)
				a.value += additionalRunningTime / 2
			}
			a.lastUpdated = a.nextHalvening
			a.nextHalvening = a.nextHalvening.Add(a.halflife)
			a.value /= 2
		}
		if a.running {
			additionalRunningTime := float64(currentTime.Sub(a.lastUpdated)) / float64(a.halflife)
			a.value += additionalRunningTime / 2
		}
	}
	a.lastUpdated = currentTime
	return a.value
}
