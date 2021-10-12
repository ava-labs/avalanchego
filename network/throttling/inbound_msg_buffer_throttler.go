// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

// See inbound_msg_throttler.go

func newInboundMsgBufferThrottler(maxProcessingMsgsPerNode uint64) *inboundMsgBufferThrottler {
	return &inboundMsgBufferThrottler{
		maxProcessingMsgsPerNode: maxProcessingMsgsPerNode,
		awaitingAcquire:          make(map[ids.ShortID][]chan struct{}),
		nodeToNumProcessingMsgs:  make(map[ids.ShortID]uint64),
	}
}

// Rate-limits inbound messages based on the number of
// messages from a given node that we're currently processing.
type inboundMsgBufferThrottler struct {
	lock sync.Mutex
	// Max number of messages currently processing from a
	// given node. We will stop reading messages from a
	// node until we're processing less than this many
	// messages from the node.
	// In this case, a message is "processing" if the corresponding
	// call to Acquire() has returned or is about to return,
	// but the corresponding call to Release() has not happened.
	// TODO: Different values for validators / non-validators?
	maxProcessingMsgsPerNode uint64
	// Node ID --> Number of messages from this node we're currently processing.
	// Must only be accessed when [lock] is held.
	nodeToNumProcessingMsgs map[ids.ShortID]uint64
	// Node ID --> Channels where each channel, when closed,
	// causes a goroutine waiting in Acquire to return.
	// The first element corresponds to the goroutine that has been waiting
	// longest to acquire space on the message buffer for the given node ID,
	// the second element the second longest, etc.
	// Must only be accessed when [lock] is held.
	awaitingAcquire map[ids.ShortID][]chan struct{}
}

// Acquire returns when we've acquired space on the inbound message
// buffer so that we can read a message from [nodeID].
// Release([nodeID]) must be called (!) when done processing the message
// (or when we give up trying to read the message.)
func (t *inboundMsgBufferThrottler) Acquire(nodeID ids.ShortID) {
	// TODO add metrics
	t.lock.Lock()
	if t.nodeToNumProcessingMsgs[nodeID] < t.maxProcessingMsgsPerNode {
		t.nodeToNumProcessingMsgs[nodeID]++
		t.lock.Unlock()
		return
	}

	// We're currently processing the maximum number of
	// messages from [nodeID]. Wait until we've finished
	// processing some messages from [nodeID].
	closeOnAcquireChan := make(chan struct{})
	t.awaitingAcquire[nodeID] = append(t.awaitingAcquire[nodeID], closeOnAcquireChan)
	t.lock.Unlock()
	<-closeOnAcquireChan
}

// Release marks that we've finished processing a message from [nodeID]
// and can release the space it took on the inbound message buffer.
func (t *inboundMsgBufferThrottler) Release(nodeID ids.ShortID) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.nodeToNumProcessingMsgs[nodeID]--
	if t.nodeToNumProcessingMsgs[nodeID] == 0 {
		delete(t.nodeToNumProcessingMsgs, nodeID)
	}

	// If we're waiting to acquire for messages from [nodeID],
	// allow the one that had been waiting the longest to
	// proceed (i.e. for its call to Acquire to return.)
	waiting := t.awaitingAcquire[nodeID]
	if len(waiting) == 0 {
		// We're not waiting to acquire for any messages from [nodeID]
		return
	}
	if len(waiting) > 0 {
		waitingLongest := waiting[0]
		t.nodeToNumProcessingMsgs[nodeID]++
		close(waitingLongest)
	}
	// Update [t.awaitingAcquire]
	if len(waiting) == 1 {
		delete(t.awaitingAcquire, nodeID)
	} else {
		t.awaitingAcquire[nodeID] = t.awaitingAcquire[nodeID][1:]
	}
}
