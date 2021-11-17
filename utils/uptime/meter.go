// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
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
