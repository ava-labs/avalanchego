// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var (
	_ MsgThrottler = &noMsgThrottler{}
	_ MsgThrottler = &sybilMsgThrottler{}
)

// MsgThrottler rate-limits incoming messages from the network.
type MsgThrottler interface {
	// Blocks until node [nodeID] can put a message of
	// size [msgSize] onto the incoming message buffer.
	Acquire(msgSize uint64, nodeID ids.ShortID)

	// Mark that a message from [nodeID] of size [msgSize]
	// has been removed from the incoming message buffer.
	// TODO use duration
	Release(msgSize uint64, nodeID ids.ShortID)
}

func newSybilMsgThrottler(
	vdrs validators.Set,
	maxUnprocessedVdrBytes uint64,
	maxUnprocessedAtLargeBytes uint64,
) MsgThrottler {
	return &sybilMsgThrottler{
		vdrs:                   vdrs,
		maxUnprocessedVdrBytes: maxUnprocessedVdrBytes,
		remainingVdrBytes:      maxUnprocessedVdrBytes,
		remainingAtLargeBytes:  maxUnprocessedAtLargeBytes,
		vdrToBytesUsed:         make(map[ids.ShortID]uint64),
	}
}

// msgThrottler implements MsgThrottler.
// It gives more space to validators with more stake.
type sybilMsgThrottler struct {
	cond sync.Cond
	// Primary network validator set
	vdrs validators.Set
	// Max number of unprocessed bytes from validators
	maxUnprocessedVdrBytes uint64
	// Number of bytes left in the validator byte allocation.
	// Initialized to [maxUnprocessedVdrBytes].
	remainingVdrBytes uint64
	// Number of bytes left in the at-large byte allocation
	remainingAtLargeBytes uint64
	// Node ID --> Bytes they've taken from the validator allocation
	vdrToBytesUsed map[ids.ShortID]uint64
}

func (t *sybilMsgThrottler) Acquire(msgSize uint64, nodeID ids.ShortID) {
	t.cond.L.Lock()
	defer t.cond.L.Unlock()

	for {
		// See if we can take from the at-large byte allocation
		if msgSize <= t.remainingAtLargeBytes {
			// Take from the at-large byte allocation
			t.remainingAtLargeBytes -= msgSize
			break
		}

		// See if we can use the validator byte allocation
		weight, isVdr := t.vdrs.GetWeight(nodeID)
		if !isVdr {
			// This node isn't a validator.
			// Wait until there are more bytes in an allocation.
			t.cond.Wait()
			continue
		}

		// Need [vdrBytesNeeded] from the validator allocation.
		// The remainder of [t.remainingAtLargeBytes], if any, will be used.
		vdrBytesNeeded := msgSize - t.remainingAtLargeBytes
		if t.remainingVdrBytes < vdrBytesNeeded {
			// Wait until there are more bytes in an allocation.
			t.cond.Wait()
			continue
		}

		// Number of bytes this node can take from validator allocation.
		vdrBytesAllowed := uint64(float64(t.maxUnprocessedVdrBytes) * float64(weight) / float64(t.vdrs.Weight()))
		if t.vdrToBytesUsed[nodeID]+vdrBytesNeeded > vdrBytesAllowed {
			// Wait until there are more bytes in an allocation.
			t.cond.Wait()
			continue
		}

		// Use the rest of [remainingAtLargeBytes] and some of [remainingVdrBytes]
		t.remainingVdrBytes -= vdrBytesNeeded
		t.remainingAtLargeBytes = 0
		t.vdrToBytesUsed[nodeID] += vdrBytesNeeded
		break
	}
}

func (t *sybilMsgThrottler) Release(msgSize uint64, nodeID ids.ShortID) {
	if msgSize == 0 {
		return // TODO this should never happen
	}
	t.cond.L.Lock()
	defer t.cond.L.Unlock()

	// Try to release these bytes back to the validator allocation
	vdrBytesUsed := t.vdrToBytesUsed[nodeID]
	switch { // This switch is exhaustive
	case vdrBytesUsed > msgSize:
		// Put all bytes back in validator allocation
		t.remainingVdrBytes += msgSize
		t.vdrToBytesUsed[nodeID] -= msgSize
	case vdrBytesUsed == msgSize:
		// Put all bytes back in validator allocation
		t.remainingVdrBytes += msgSize
		delete(t.vdrToBytesUsed, nodeID)
	case vdrBytesUsed < msgSize && vdrBytesUsed > 0:
		// Put some bytes back in validator allocation
		t.remainingVdrBytes += vdrBytesUsed
		t.remainingAtLargeBytes += msgSize - vdrBytesUsed
		delete(t.vdrToBytesUsed, nodeID)
	case vdrBytesUsed < msgSize && vdrBytesUsed == 0:
		// Put no bytes in validator allocation
		t.remainingAtLargeBytes += msgSize
	}

	// Notify that there are more bytes available
	t.cond.Broadcast()
}

// noMsgThrottler implements MsgThrottler.
// [Acquire] always returns immediately.
type noMsgThrottler struct{}

func (*noMsgThrottler) Acquire(uint64, ids.ShortID) {}

func (*noMsgThrottler) Release(uint64, ids.ShortID) {}
