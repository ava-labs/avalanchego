// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

type unprocessedMsgs interface {
	// Add an unprocessed message
	Push(message)
	// Get and remove the unprocessed message that should
	// be processed next. Must never be called when Len() == 0.
	Pop() message
	// Returns the number of unprocessed messages
	Len() int
}

func newUnprocessedMsgs(
	log logging.Logger,
	vdrs validators.Set,
	cpuTracker tracker.TimeTracker,
) unprocessedMsgs {
	return &unprocessedMsgsImpl{
		log:                   log,
		vdrs:                  vdrs,
		cpuTracker:            cpuTracker,
		nodeToUnprocessedMsgs: make(map[ids.ShortID]int),
	}
}

// Implements unprocessedMsgs.
// Not safe for concurrent access.
type unprocessedMsgsImpl struct {
	log logging.Logger
	// Node ID --> Messages this node has in [msgs]
	nodeToUnprocessedMsgs map[ids.ShortID]int
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
	u.nodeToUnprocessedMsgs[msg.nodeID]++
}

// Must never be called when [u.Len()] == 0
func (u *unprocessedMsgsImpl) Pop() message {
	n := len(u.msgs)
	i := 0
	for {
		if i == n {
			// This should never happen but handle anyway
			u.log.Warn("canPop is false for all %d unprocessed messages", n)
		}
		msg := u.msgs[0]
		// See if it's OK to process [msg] next
		if u.canPop(&msg) || i == n {
			if len(u.msgs) == 1 {
				u.msgs = nil // Give back memory if possible
			} else {
				u.msgs = u.msgs[1:]
			}
			u.nodeToUnprocessedMsgs[msg.nodeID]--
			if u.nodeToUnprocessedMsgs[msg.nodeID] == 0 {
				delete(u.nodeToUnprocessedMsgs, msg.nodeID)
			}
			return msg
		}
		// Push [msg] to back of [u.msgs]
		u.msgs = append(u.msgs, msg)
		u.msgs = u.msgs[1:]
		i++
	}
}

func (u *unprocessedMsgsImpl) Len() int {
	return len(u.msgs)
}

// canPop will return true for at least one message in [u.msgs]
func (u *unprocessedMsgsImpl) canPop(msg *message) bool {
	// Every node has some allowed CPU allocation depending on
	// the number of nodes with unprocessed messages.
	baseMaxCPU := 1 / float64(len(u.nodeToUnprocessedMsgs))
	weight, isVdr := u.vdrs.GetWeight(msg.nodeID)
	if !isVdr {
		weight = 0
	}
	// The sum of validator weights should never be 0, but handle
	// that case for completeness here to avoid divide by 0.
	portionWeight := float64(0)
	totalVdrsWeight := u.vdrs.Weight()
	if totalVdrsWeight != 0 {
		portionWeight = float64(weight) / float64(totalVdrsWeight)
	}
	// Validators are allowed to use more CPU
	recentCPUUtilized := u.cpuTracker.Utilization(msg.nodeID, u.clock.Time())
	maxCPU := baseMaxCPU + (1.0-baseMaxCPU)*portionWeight
	return recentCPUUtilized <= maxCPU
}
