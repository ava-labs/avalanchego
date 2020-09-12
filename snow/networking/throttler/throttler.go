// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttler

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

const (
	// DefaultMaxNonStakerPendingMsgs rate limits the number of queued messages
	// from non-stakers.
	DefaultMaxNonStakerPendingMsgs uint32 = 3

	// DefaultStakerPortion describes the percentage of resources that are
	// reserved for stakers.
	DefaultStakerPortion float64 = 0.2
)

// Throttler provides an interface to register consumption
// of resources and prioritize messages from nodes that have
// used less CPU time.
type Throttler interface {
	AddMessage(ids.ShortID)
	RemoveMessage(ids.ShortID)
	UtilizeCPU(ids.ShortID, time.Duration)
	GetUtilization(ids.ShortID) (float64, bool) // Returns the CPU based priority and whether or not the peer has too many pending messages
	EndInterval()                               // Notify throttler that the current period has ended
}

// CPUTracker tracks the consumption of CPU time
type CPUTracker interface {
	UtilizeCPU(ids.ShortID, time.Duration)
	GetUtilization(ids.ShortID) float64
	EndInterval()
}

// CountingThrottler tracks the usage of a discrete resource (ex. pending messages) by a peer
// and determines whether or not a peer should be throttled.
type CountingThrottler interface {
	Add(ids.ShortID)
	Remove(ids.ShortID)
	Throttle(ids.ShortID) bool
	EndInterval()
}
