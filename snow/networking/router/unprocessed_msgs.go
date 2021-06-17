// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"sync"

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
	// Returns a condition variable that is signalled when
	// there are unprocessed messages or when Shutdown is called
	Cond() *sync.Cond
	// Signals the condition variable returned by Cond()
	// and causes HasShutdown() to return true
	Shutdown()
	// Returns true iff Shutdown() has been called
	HasShutdown() bool
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
	vdrs validators.Set
	// TODO periodically call [cpuTracker.EndInterval]
	cpuTracker tracker.TimeTracker
	// Useful for faking time in tests
	clock timer.Clock
	cond  sync.Cond
	// true iff Shutdown() has been called
	hasShutdown bool
}

func (u *unprocessedMsgsImpl) Push(msg message) {
	u.msgs = append(u.msgs, msg)
	u.cond.Signal()
}

func (u *unprocessedMsgsImpl) Shutdown() {
	u.cond.Signal()
	u.hasShutdown = true
}

func (u *unprocessedMsgsImpl) HasShutdown() bool {
	return u.hasShutdown
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

func (u *unprocessedMsgsImpl) Cond() *sync.Cond {
	return &u.cond
}

func (u *unprocessedMsgsImpl) Len() int {
	return len(u.msgs)
}

func (u *unprocessedMsgsImpl) canPop(msg *message) bool {
	recentCPUUtilized := u.cpuTracker.Utilization(msg.validatorID, u.clock.Time())
	weight, isVdr := u.vdrs.GetWeight(msg.validatorID)
	if isVdr {
		portionWeight := float64(weight) / float64(u.vdrs.Weight())
		if recentCPUUtilized <= portionWeight {
			return true
		}
	}
	if recentCPUUtilized <= 1/float64(len(u.msgs)) {
		return true
	}
	return false
}
