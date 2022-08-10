// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

// Test inboundMsgBufferThrottler
func TestMsgBufferThrottler(t *testing.T) {
	assert := assert.New(t)
	throttler, err := newInboundMsgBufferThrottler("", prometheus.NewRegistry(), 3)
	assert.NoError(err)

	nodeID1, nodeID2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	// Acquire shouldn't block for first 3
	throttler.Acquire(context.Background(), nodeID1)
	throttler.Acquire(context.Background(), nodeID1)
	throttler.Acquire(context.Background(), nodeID1)
	assert.Len(throttler.nodeToNumProcessingMsgs, 1)
	assert.EqualValues(3, throttler.nodeToNumProcessingMsgs[nodeID1])

	// Acquire shouldn't block for other node
	throttler.Acquire(context.Background(), nodeID2)
	throttler.Acquire(context.Background(), nodeID2)
	throttler.Acquire(context.Background(), nodeID2)
	assert.Len(throttler.nodeToNumProcessingMsgs, 2)
	assert.EqualValues(3, throttler.nodeToNumProcessingMsgs[nodeID1])
	assert.EqualValues(3, throttler.nodeToNumProcessingMsgs[nodeID2])

	// Acquire should block for 4th acquire
	done := make(chan struct{})
	go func() {
		throttler.Acquire(context.Background(), nodeID1)
		done <- struct{}{}
	}()
	select {
	case <-done:
		t.Fatal("should block on acquiring")
	case <-time.After(50 * time.Millisecond):
	}

	throttler.release(nodeID1)
	// fourth acquire should be unblocked
	<-done
	assert.Len(throttler.nodeToNumProcessingMsgs, 2)
	assert.EqualValues(3, throttler.nodeToNumProcessingMsgs[nodeID2])

	// Releasing from other node should have no effect
	throttler.release(nodeID2)
	throttler.release(nodeID2)
	throttler.release(nodeID2)

	// Release remaining 3 acquires
	throttler.release(nodeID1)
	throttler.release(nodeID1)
	throttler.release(nodeID1)
	assert.Len(throttler.nodeToNumProcessingMsgs, 0)
}

// Test inboundMsgBufferThrottler when an acquire is cancelled
func TestMsgBufferThrottlerContextCancelled(t *testing.T) {
	assert := assert.New(t)
	throttler, err := newInboundMsgBufferThrottler("", prometheus.NewRegistry(), 3)
	assert.NoError(err)

	vdr1Context, vdr1ContextCancelFunc := context.WithCancel(context.Background())
	nodeID1 := ids.GenerateTestNodeID()
	// Acquire shouldn't block for first 3
	throttler.Acquire(vdr1Context, nodeID1)
	throttler.Acquire(vdr1Context, nodeID1)
	throttler.Acquire(vdr1Context, nodeID1)
	assert.Len(throttler.nodeToNumProcessingMsgs, 1)
	assert.EqualValues(3, throttler.nodeToNumProcessingMsgs[nodeID1])

	// Acquire should block for 4th acquire
	done := make(chan struct{})
	go func() {
		throttler.Acquire(vdr1Context, nodeID1)
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
		throttler.Acquire(vdr1Context, nodeID1)
		done2 <- struct{}{}
	}()
	select {
	case <-done2:
		t.Fatal("should block on acquiring")
	case <-time.After(50 * time.Millisecond):
	}

	// Unblock fifth acquire
	vdr1ContextCancelFunc()
	select {
	case <-done2:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("cancelling context should unblock Acquire")
	}
	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("should be blocked")
	}
}
