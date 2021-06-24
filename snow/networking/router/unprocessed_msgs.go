// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer"
)

// TODO move to different package?
type unprocessedMsgs interface {
	// Add an unprocessed message
	Push(message)
	// Get and remove the unprocessed message that should
	// be processed next
	Pop() message
	// Returns the number of unprocessed messages
	Len() int
}

func newUnprocessedMsgs(vdrs validators.Set, cpuTracker tracker.TimeTracker) unprocessedMsgs {
	return &unprocessedMsgsImpl{
		vdrs:       vdrs,
		cpuTracker: cpuTracker,
	}
}

// Implements unprocessedMsgs.
// Not safe for concurrent access.
type unprocessedMsgsImpl struct {
	// unprocessed messages
	msgs []message
	// Validator set for the chain associated with this
	vdrs       validators.Set
	cpuTracker tracker.TimeTracker
	// Useful for faking time in tests
	clock timer.Clock
}

func (u *unprocessedMsgsImpl) Push(msg message) {
	u.msgs = append(u.msgs, msg)
}

// Must only be called when [u.Len()] != 0
func (u *unprocessedMsgsImpl) Pop() message {
	// TODO make sure this always terminates
	// Is it possible that calls to Utilization sum to more than 1?
	for {
		msg := u.msgs[0]
		if u.canPop(&msg) {
			if len(u.msgs) == 1 {
				u.msgs = nil // Give back memory if possible
			} else {
				u.msgs = u.msgs[1:]
			}
			return msg
		}
		u.msgs = append(u.msgs, msg)
		u.msgs = u.msgs[1:]
	}
}

func (u *unprocessedMsgsImpl) Len() int {
	return len(u.msgs)
}

// canPop will return true for at least one message in [u.msgs]
func (u *unprocessedMsgsImpl) canPop(msg *message) bool {
	baseMaxCPU := 1 / float64(len(u.msgs))
	weight, isVdr := u.vdrs.GetWeight(msg.validatorID)
	if !isVdr {
		weight = 0
	}
	portionWeight := float64(weight) / float64(u.vdrs.Weight())
	recentCPUUtilized := u.cpuTracker.Utilization(msg.validatorID, u.clock.Time())
	maxCPU := baseMaxCPU + (1-baseMaxCPU)*portionWeight
	return recentCPUUtilized <= maxCPU
}
