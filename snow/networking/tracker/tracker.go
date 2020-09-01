// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"time"

	"github.com/ava-labs/gecko/ids"
)

// TimeTracker is an interface for tracking peers' usage of CPU Time
type TimeTracker interface {
	UtilizeTime(ids.ShortID, time.Duration)
	GetUtilization(ids.ShortID) float64
	TotalUtilization() float64
	EndInterval()
}

// CountingTracker is an interface for tracking peers' usage of a discrete resource
type CountingTracker interface {
	Add(ids.ShortID)
	Remove(ids.ShortID)
	OutstandingCount(ids.ShortID) uint32
}
