// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"testing"
	"time"

	"github.com/chain4travel/caminogo/ids"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

// Test inboundMsgBufferThrottler
func TestMsgBufferThrottler(t *testing.T) {
	assert := assert.New(t)
	throttler, err := newInboundMsgBufferThrottler("", prometheus.NewRegistry(), 3)
	assert.NoError(err)

	nodeID1, nodeID2 := ids.GenerateTestShortID(), ids.GenerateTestShortID()
	// Acquire shouldn't block for first 3
	throttler.Acquire(nodeID1)
	throttler.Acquire(nodeID1)
	throttler.Acquire(nodeID1)
	assert.Len(throttler.nodeToNumProcessingMsgs, 1)
	assert.EqualValues(3, throttler.nodeToNumProcessingMsgs[nodeID1])

	// Acquire shouldn't block for other node
	throttler.Acquire(nodeID2)
	throttler.Acquire(nodeID2)
	throttler.Acquire(nodeID2)
	assert.Len(throttler.nodeToNumProcessingMsgs, 2)
	assert.EqualValues(3, throttler.nodeToNumProcessingMsgs[nodeID1])
	assert.EqualValues(3, throttler.nodeToNumProcessingMsgs[nodeID2])

	// Acquire should block for 4th acquire
	done := make(chan struct{})
	go func() {
		throttler.Acquire(nodeID1)
		done <- struct{}{}
	}()
	select {
	case <-done:
		t.Fatal("should block on acquiring")
	case <-time.After(50 * time.Millisecond):
	}

	// Acquire should block for 5th acquire
	done2 := make(chan struct{})
	go func() {
		throttler.Acquire(nodeID1)
		done2 <- struct{}{}
	}()
	select {
	case <-done2:
		t.Fatal("should block on acquiring")
	case <-time.After(50 * time.Millisecond):
	}

	throttler.Release(nodeID1)
	// fourth acquire should be unblocked
	<-done
	assert.Len(throttler.nodeToNumProcessingMsgs, 2)
	assert.EqualValues(3, throttler.nodeToNumProcessingMsgs[nodeID2])

	// But not the other
	select {
	case <-done2:
		t.Fatal("should be blocked")
	case <-time.After(50 * time.Millisecond):
	}

	// Releasing from other node should have no effect
	throttler.Release(nodeID2)
	throttler.Release(nodeID2)
	throttler.Release(nodeID2)
	select {
	case <-done2:
		t.Fatal("should be blocked")
	case <-time.After(50 * time.Millisecond):
	}

	// Unblock fifth acquire
	throttler.Release(nodeID1)
	<-done2
	// Release remaining 3 acquires
	throttler.Release(nodeID1)
	throttler.Release(nodeID1)
	throttler.Release(nodeID1)
	assert.Len(throttler.nodeToNumProcessingMsgs, 0)
}
