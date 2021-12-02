// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"
)

// Factory returns new meters.
type Factory interface {
	// New returns a new meter with the provided halflife.
	New(halflife time.Duration) Meter
}
