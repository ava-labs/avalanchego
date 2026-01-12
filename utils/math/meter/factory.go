// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meter

import "time"

// Factory returns new meters.
type Factory interface {
	// New returns a new meter with the provided halflife.
	New(halflife time.Duration) Meter
}
