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
	Release(msgSize uint64, nodeID ids.ShortID)
}

// msgThrottler implements MsgThrottler.
// It gives more space to validators with more stake.
type sybilMsgThrottler struct {
	cond                   sync.Cond
	vdrs                   validators.Set
	maxUnprocessedVdrBytes uint64
	remainingVdrBytes      uint64
	remainingAtLargeBytes  uint64
	vdrToBytesUsed         map[ids.ShortID]uint64
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
		if isVdr && t.remainingVdrBytes > msgSize {
			bytesAllowed := uint64(float64(t.maxUnprocessedVdrBytes) * float64(weight) / float64(t.vdrs.Weight()))
			if t.vdrToBytesUsed[nodeID]+msgSize <= bytesAllowed {
				// Take from the validator byte allocation
				t.remainingVdrBytes -= msgSize
				t.vdrToBytesUsed[nodeID] += msgSize
				break
			}
		}

		// Wait until some space is released
		t.cond.Wait()
	}
}

func (t *sybilMsgThrottler) Release(uint64, ids.ShortID) {
	// TODO
}

// noMsgThrottler implements MsgThrottler.
// [Acquire] always returns immediately.
type noMsgThrottler struct{}

func (*noMsgThrottler) Acquire(uint64, ids.ShortID) {}

func (*noMsgThrottler) Release(uint64, ids.ShortID) {}
