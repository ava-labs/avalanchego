// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// Test inboundMsgBufferThrottler
func TestMsgBufferThrottler(t *testing.T) {
	require := require.New(t)
	throttler, err := newInboundMsgBufferThrottler(prometheus.NewRegistry(), 3)
	require.NoError(err)

	nodeID1, nodeID2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	// Acquire shouldn't block for first 3
	throttler.Acquire(t.Context(), nodeID1)
	throttler.Acquire(t.Context(), nodeID1)
	throttler.Acquire(t.Context(), nodeID1)
	require.Len(throttler.nodeToNumProcessingMsgs, 1)
	require.Equal(uint64(3), throttler.nodeToNumProcessingMsgs[nodeID1])

	// Acquire shouldn't block for other node
	throttler.Acquire(t.Context(), nodeID2)
	throttler.Acquire(t.Context(), nodeID2)
	throttler.Acquire(t.Context(), nodeID2)
	require.Len(throttler.nodeToNumProcessingMsgs, 2)
	require.Equal(uint64(3), throttler.nodeToNumProcessingMsgs[nodeID1])
	require.Equal(uint64(3), throttler.nodeToNumProcessingMsgs[nodeID2])

	// Acquire should block for 4th acquire
	done := make(chan struct{})
	go func() {
		throttler.Acquire(t.Context(), nodeID1)
		done <- struct{}{}
	}()
	select {
	case <-done:
		require.FailNow("should block on acquiring")
	case <-time.After(50 * time.Millisecond):
	}

	throttler.release(nodeID1)
	// fourth acquire should be unblocked
	<-done
	require.Len(throttler.nodeToNumProcessingMsgs, 2)
	require.Equal(uint64(3), throttler.nodeToNumProcessingMsgs[nodeID2])

	// Releasing from other node should have no effect
	throttler.release(nodeID2)
	throttler.release(nodeID2)
	throttler.release(nodeID2)

	// Release remaining 3 acquires
	throttler.release(nodeID1)
	throttler.release(nodeID1)
	throttler.release(nodeID1)
	require.Empty(throttler.nodeToNumProcessingMsgs)
}

// Test inboundMsgBufferThrottler when an acquire is cancelled
func TestMsgBufferThrottlerContextCancelled(t *testing.T) {
	require := require.New(t)
	throttler, err := newInboundMsgBufferThrottler(prometheus.NewRegistry(), 3)
	require.NoError(err)

	vdr1Context, vdr1ContextCancelFunc := context.WithCancel(t.Context())
	nodeID1 := ids.GenerateTestNodeID()
	// Acquire shouldn't block for first 3
	throttler.Acquire(vdr1Context, nodeID1)
	throttler.Acquire(vdr1Context, nodeID1)
	throttler.Acquire(vdr1Context, nodeID1)
	require.Len(throttler.nodeToNumProcessingMsgs, 1)
	require.Equal(uint64(3), throttler.nodeToNumProcessingMsgs[nodeID1])

	// Acquire should block for 4th acquire
	done := make(chan struct{})
	go func() {
		throttler.Acquire(vdr1Context, nodeID1)
		done <- struct{}{}
	}()
	select {
	case <-done:
		require.FailNow("should block on acquiring")
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
		require.FailNow("should block on acquiring")
	case <-time.After(50 * time.Millisecond):
	}

	// Unblock fifth acquire
	vdr1ContextCancelFunc()
	select {
	case <-done2:
	case <-time.After(50 * time.Millisecond):
		require.FailNow("cancelling context should unblock Acquire")
	}
	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		require.FailNow("should be blocked")
	}
}
