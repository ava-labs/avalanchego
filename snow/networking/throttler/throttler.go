// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttler

import (
	"time"

	"github.com/ava-labs/gecko/ids"
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
