// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"
)

const (
	maxSkippedIntervals = 32
)

var (
	_ Meter   = &intervalMeter{}
	_ Factory = &IntervalFactory{}
)

// IntervalFactory implements the Factory interface by returning an interval
// based meter.
type IntervalFactory struct{}

// New implements the Factory interface.
func (IntervalFactory) New(halflife time.Duration) Meter {
	return NewIntervalMeter(halflife)
}

type intervalMeter struct {
	running bool

	halflife       time.Duration
	previousValues time.Duration
	currentValue   time.Duration
	nextHalvening  time.Time
	lastUpdated    time.Time
}

// NewIntervalMeter returns a new Meter with the provided halflife
func NewIntervalMeter(halflife time.Duration) Meter {
	return &intervalMeter{halflife: halflife}
}

func (a *intervalMeter) Start(currentTime time.Time) {
	a.update(currentTime, true)
}

func (a *intervalMeter) Stop(currentTime time.Time) {
	a.update(currentTime, false)
}

func (a *intervalMeter) update(currentTime time.Time, running bool) {
	if a.running == running {
		return
	}
	a.Read(currentTime)
	a.running = running
}

func (a *intervalMeter) Read(currentTime time.Time) float64 {
	if currentTime.After(a.lastUpdated) {
		// try to finish the current round
		if !currentTime.Before(a.nextHalvening) {
			if a.running {
				a.currentValue += a.nextHalvening.Sub(a.lastUpdated)
			}
			a.lastUpdated = a.nextHalvening
			a.nextHalvening = a.nextHalvening.Add(a.halflife)
			a.previousValues += a.currentValue >> 1
			a.currentValue = 0
			a.previousValues >>= 1

			// try to skip future rounds
			if totalTime := currentTime.Sub(a.lastUpdated); totalTime >= a.halflife {
				numSkippedPeriods := totalTime / a.halflife
				if numSkippedPeriods > maxSkippedIntervals {
					a.lastUpdated = currentTime
					a.nextHalvening = currentTime.Add(a.halflife)

					// If this meter hasn't been read in a long time, avoid
					// potential shifting overflow issues and just jump to a
					// reasonable value.
					a.currentValue = 0
					if a.running {
						a.previousValues = a.halflife >> 1
						return 1
					}
					a.previousValues = 0
					return 0
				}

				if numSkippedPeriods > 0 {
					a.previousValues >>= numSkippedPeriods
					if a.running {
						additionalRunningTime := a.halflife - (a.halflife >> numSkippedPeriods)
						a.previousValues += additionalRunningTime >> 1
					}
					skippedDuration := numSkippedPeriods * a.halflife
					a.lastUpdated = a.lastUpdated.Add(skippedDuration)
					a.nextHalvening = a.nextHalvening.Add(skippedDuration)
				}
			}
		}

		// increment the value for the current round
		if a.running {
			a.currentValue += currentTime.Sub(a.lastUpdated)
		}
		a.lastUpdated = currentTime
	}

	spentTime := a.halflife - a.nextHalvening.Sub(a.lastUpdated)
	if spentTime == 0 {
		return float64(2*a.previousValues) / float64(a.halflife)
	}
	spentTime <<= 1
	expectedValue := float64(a.currentValue) / float64(spentTime)
	return float64(a.previousValues)/float64(a.halflife) + expectedValue
}
