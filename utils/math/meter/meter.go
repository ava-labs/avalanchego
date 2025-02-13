// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meter

import "time"

// Meter tracks a continuous exponential moving average of the % of time this
// meter has been running.
type Meter interface {
	// Inc the meter, the read value will be monotonically increasing while
	// the meter is running.
	Inc(time.Time, float64)

	// Dec the meter, the read value will be exponentially decreasing while the
	// meter is off.
	Dec(time.Time, float64)

	// Read the current value of the meter, this can be used to approximate the
	// percent of time the meter has been running recently. The definition of
	// recently depends on the halflife of the decay function.
	Read(time.Time) float64

	// Returns the duration between [now] and when the value of this meter
	// reaches [value], assuming that the number of cores running is always 0.
	// If the value of this meter is already <= [value], returns the zero duration.
	TimeUntil(now time.Time, value float64) time.Duration
}
