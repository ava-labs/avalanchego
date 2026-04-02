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
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestInboundMsgByteThrottlerCancelContextDeadlock(t *testing.T) {
	require := require.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1,
		AtLargeAllocSize:    1,
		NodeMaxAtLargeBytes: 1,
	}
	vdrs := validators.NewManager()
	vdr := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr, nil, ids.Empty, 1))

	throttler, err := newInboundMsgByteThrottler(
		logging.NoLog{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	require.NoError(err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	nodeID := ids.GenerateTestNodeID()
	release := throttler.Acquire(ctx, 2, nodeID)
	release()
}

func TestInboundMsgByteThrottlerCancelContext(t *testing.T) {
	require := require.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1024,
		AtLargeAllocSize:    512,
		NodeMaxAtLargeBytes: 1024,
	}
	vdrs := validators.NewManager()
	vdr1ID := ids.GenerateTestNodeID()
	vdr2ID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr1ID, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr2ID, nil, ids.Empty, 1))

	throttler, err := newInboundMsgByteThrottler(
		logging.NoLog{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	require.NoError(err)

	throttler.Acquire(t.Context(), config.VdrAllocSize, vdr1ID)

	// Trying to take more bytes for node should block
	vdr2Done := make(chan struct{})
	vdr2Context, vdr2ContextCancelFunction := context.WithCancel(t.Context())
	go func() {
		throttler.Acquire(vdr2Context, config.VdrAllocSize, vdr2ID)
		vdr2Done <- struct{}{}
	}()
	select {
	case <-vdr2Done:
		require.FailNow("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// ensure the throttler has recorded that vdr2 is waiting
	throttler.lock.Lock()
	require.Len(throttler.nodeToWaitingMsgID, 1)
	require.Contains(throttler.nodeToWaitingMsgID, vdr2ID)
	require.Equal(1, throttler.waitingToAcquire.Len())
	_, exists := throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgID[vdr2ID])
	require.True(exists)
	throttler.lock.Unlock()

	// cancel should cause vdr2's acquire to unblock
	vdr2ContextCancelFunction()

	select {
	case <-vdr2Done:
	case <-time.After(50 * time.Millisecond):
		require.FailNow("channel should signal because ctx was cancelled")
	}

	require.NotContains(throttler.nodeToWaitingMsgID, vdr2ID)
}

func TestInboundMsgByteThrottler(t *testing.T) {
	require := require.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1024,
		AtLargeAllocSize:    1024,
		NodeMaxAtLargeBytes: 1024,
	}
	vdrs := validators.NewManager()
	vdr1ID := ids.GenerateTestNodeID()
	vdr2ID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr1ID, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr2ID, nil, ids.Empty, 1))

	throttler, err := newInboundMsgByteThrottler(
		logging.NoLog{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	require.NoError(err)

	// Make sure NewSybilInboundMsgThrottler works
	require.Equal(config.VdrAllocSize, throttler.maxVdrBytes)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	require.NotNil(throttler.nodeToVdrBytesUsed)
	require.NotNil(throttler.log)
	require.NotNil(throttler.vdrs)
	require.NotNil(throttler.metrics)

	// Take from at-large allocation.
	// Should return immediately.
	throttler.Acquire(t.Context(), 1, vdr1ID)
	require.Equal(config.AtLargeAllocSize-1, throttler.remainingAtLargeBytes)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1)
	require.Equal(uint64(1), throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Release the bytes
	throttler.release(&msgMetadata{msgSize: 1}, vdr1ID)
	require.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Empty(throttler.nodeToAtLargeBytesUsed)

	// Use all the at-large allocation bytes and 1 of the validator allocation bytes
	// Should return immediately.
	throttler.Acquire(t.Context(), config.AtLargeAllocSize+1, vdr1ID)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 1
	require.Zero(throttler.remainingAtLargeBytes)
	require.Equal(config.VdrAllocSize-1, throttler.remainingVdrBytes)
	require.Equal(uint64(1), throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Len(throttler.nodeToVdrBytesUsed, 1)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1)
	require.Equal(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// The other validator should be able to acquire half the validator allocation.
	// Should return immediately.
	throttler.Acquire(t.Context(), config.AtLargeAllocSize/2, vdr2ID)
	// vdr2 at-large bytes used: 0. Validator bytes used: 512
	require.Equal(config.VdrAllocSize/2-1, throttler.remainingVdrBytes)
	require.Equal(uint64(1), throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Equal(config.VdrAllocSize/2, throttler.nodeToVdrBytesUsed[vdr2ID])
	require.Len(throttler.nodeToVdrBytesUsed, 2)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1)
	require.Empty(throttler.nodeToWaitingMsgID)
	require.Zero(throttler.waitingToAcquire.Len())

	// vdr1 should be able to acquire the rest of the validator allocation
	// Should return immediately.
	throttler.Acquire(t.Context(), config.VdrAllocSize/2-1, vdr1ID)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 512
	require.Equal(config.VdrAllocSize/2, throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Len(throttler.nodeToAtLargeBytesUsed, 1)
	require.Equal(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Trying to take more bytes for either node should block
	vdr1Done := make(chan struct{})
	go func() {
		throttler.Acquire(t.Context(), 1, vdr1ID)
		vdr1Done <- struct{}{}
	}()
	select {
	case <-vdr1Done:
		require.FailNow("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}
	throttler.lock.Lock()
	require.Len(throttler.nodeToWaitingMsgID, 1)
	require.Contains(throttler.nodeToWaitingMsgID, vdr1ID)
	require.Equal(1, throttler.waitingToAcquire.Len())
	_, exists := throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgID[vdr1ID])
	require.True(exists)
	throttler.lock.Unlock()

	vdr2Done := make(chan struct{})
	go func() {
		throttler.Acquire(t.Context(), 1, vdr2ID)
		vdr2Done <- struct{}{}
	}()
	select {
	case <-vdr2Done:
		require.FailNow("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}
	throttler.lock.Lock()
	require.Len(throttler.nodeToWaitingMsgID, 2)

	require.Contains(throttler.nodeToWaitingMsgID, vdr2ID)
	require.Equal(2, throttler.waitingToAcquire.Len())
	_, exists = throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgID[vdr2ID])
	require.True(exists)
	throttler.lock.Unlock()

	nonVdrID := ids.GenerateTestNodeID()
	nonVdrDone := make(chan struct{})
	go func() {
		throttler.Acquire(t.Context(), 1, nonVdrID)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		require.FailNow("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}
	throttler.lock.Lock()
	require.Len(throttler.nodeToWaitingMsgID, 3)
	require.Contains(throttler.nodeToWaitingMsgID, nonVdrID)
	require.Equal(3, throttler.waitingToAcquire.Len())
	_, exists = throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgID[nonVdrID])
	require.True(exists)
	throttler.lock.Unlock()

	// Release config.MaxAtLargeBytes+1 bytes
	// When the choice exists, bytes should be given back to the validator allocation
	// rather than the at-large allocation.
	throttler.release(&msgMetadata{msgSize: config.AtLargeAllocSize + 1}, vdr1ID)

	// The Acquires that blocked above should have returned
	<-vdr1Done
	<-vdr2Done
	<-nonVdrDone

	require.Equal(config.NodeMaxAtLargeBytes/2, throttler.remainingVdrBytes)
	require.Len(throttler.nodeToAtLargeBytesUsed, 3) // vdr1, vdr2, nonVdrID
	require.Equal(config.AtLargeAllocSize/2, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	require.Equal(uint64(1), throttler.nodeToAtLargeBytesUsed[vdr2ID])
	require.Equal(uint64(1), throttler.nodeToAtLargeBytesUsed[nonVdrID])
	require.Len(throttler.nodeToVdrBytesUsed, 1)
	require.Zero(throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Equal(config.AtLargeAllocSize/2-2, throttler.remainingAtLargeBytes)
	require.Empty(throttler.nodeToWaitingMsgID)
	require.Zero(throttler.waitingToAcquire.Len())

	// Non-validator should be able to take the rest of the at-large bytes
	throttler.Acquire(t.Context(), config.AtLargeAllocSize/2-2, nonVdrID)
	require.Zero(throttler.remainingAtLargeBytes)
	require.Equal(config.AtLargeAllocSize/2-1, throttler.nodeToAtLargeBytesUsed[nonVdrID])
	require.Empty(throttler.nodeToWaitingMsgID)
	require.Zero(throttler.waitingToAcquire.Len())

	// But should block on subsequent Acquires
	go func() {
		throttler.Acquire(t.Context(), 1, nonVdrID)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		require.FailNow("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}
	throttler.lock.Lock()
	require.Contains(throttler.nodeToWaitingMsgID, nonVdrID)
	require.Contains(throttler.nodeToWaitingMsgID, nonVdrID)
	require.Equal(1, throttler.waitingToAcquire.Len())
	_, exists = throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgID[nonVdrID])
	require.True(exists)
	throttler.lock.Unlock()

	// Release all of vdr2's messages
	throttler.release(&msgMetadata{msgSize: config.AtLargeAllocSize / 2}, vdr2ID)
	throttler.release(&msgMetadata{msgSize: 1}, vdr2ID)

	<-nonVdrDone

	require.Zero(throttler.nodeToAtLargeBytesUsed[vdr2ID])
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Zero(throttler.remainingAtLargeBytes)
	require.NotContains(throttler.nodeToWaitingMsgID, nonVdrID)
	require.Zero(throttler.waitingToAcquire.Len())

	// Release all of vdr1's messages
	throttler.release(&msgMetadata{msgSize: 1}, vdr1ID)
	throttler.release(&msgMetadata{msgSize: config.AtLargeAllocSize/2 - 1}, vdr1ID)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Equal(config.AtLargeAllocSize/2, throttler.remainingAtLargeBytes)
	require.Zero(throttler.nodeToAtLargeBytesUsed[vdr1ID])
	require.NotContains(throttler.nodeToWaitingMsgID, nonVdrID)
	require.Zero(throttler.waitingToAcquire.Len())

	// Release nonVdr's messages
	throttler.release(&msgMetadata{msgSize: 1}, nonVdrID)
	throttler.release(&msgMetadata{msgSize: 1}, nonVdrID)
	throttler.release(&msgMetadata{msgSize: config.AtLargeAllocSize/2 - 2}, nonVdrID)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	require.Empty(throttler.nodeToAtLargeBytesUsed)
	require.Zero(throttler.nodeToAtLargeBytesUsed[nonVdrID])
	require.NotContains(throttler.nodeToWaitingMsgID, nonVdrID)
	require.Zero(throttler.waitingToAcquire.Len())
}

// Ensure that the limit on taking from the at-large allocation is enforced
func TestSybilMsgThrottlerMaxNonVdr(t *testing.T) {
	require := require.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        100,
		AtLargeAllocSize:    100,
		NodeMaxAtLargeBytes: 10,
	}
	vdrs := validators.NewManager()
	vdr1ID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr1ID, nil, ids.Empty, 1))
	throttler, err := newInboundMsgByteThrottler(
		logging.NoLog{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	require.NoError(err)
	nonVdrNodeID1 := ids.GenerateTestNodeID()
	throttler.Acquire(t.Context(), config.NodeMaxAtLargeBytes, nonVdrNodeID1)

	// Acquiring more should block
	nonVdrDone := make(chan struct{})
	go func() {
		throttler.Acquire(t.Context(), 1, nonVdrNodeID1)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		require.FailNow("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// A different non-validator should be able to acquire
	nonVdrNodeID2 := ids.GenerateTestNodeID()
	throttler.Acquire(t.Context(), config.NodeMaxAtLargeBytes, nonVdrNodeID2)

	// Validator should only be able to take [MaxAtLargeBytes]
	throttler.Acquire(t.Context(), config.NodeMaxAtLargeBytes+1, vdr1ID)
	require.Equal(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	require.Equal(uint64(1), throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Equal(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID1])
	require.Equal(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID2])
	require.Equal(config.AtLargeAllocSize-config.NodeMaxAtLargeBytes*3, throttler.remainingAtLargeBytes)
}

// Test that messages waiting to be acquired by a given node execute next
func TestMsgThrottlerNextMsg(t *testing.T) {
	require := require.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1024,
		AtLargeAllocSize:    1024,
		NodeMaxAtLargeBytes: 1024,
	}
	vdrs := validators.NewManager()
	vdr1ID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr1ID, nil, ids.Empty, 1))
	nonVdrNodeID := ids.GenerateTestNodeID()

	maxVdrBytes := config.VdrAllocSize + config.AtLargeAllocSize
	maxBytes := maxVdrBytes
	throttler, err := newInboundMsgByteThrottler(
		logging.NoLog{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	require.NoError(err)

	// validator uses up all but 1 byte
	throttler.Acquire(t.Context(), maxBytes-1, vdr1ID)
	// validator uses the last byte
	throttler.Acquire(t.Context(), 1, vdr1ID)

	// validator wants to acquire a lot of bytes
	doneVdr := make(chan struct{})
	go func() {
		throttler.Acquire(t.Context(), maxBytes-1, vdr1ID)
		doneVdr <- struct{}{}
	}()
	select {
	case <-doneVdr:
		require.FailNow("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// nonvalidator tries to acquire more bytes
	done := make(chan struct{})
	go func() {
		throttler.Acquire(t.Context(), 1, nonVdrNodeID)
		done <- struct{}{}
	}()
	select {
	case <-done:
		require.FailNow("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// Release 1 byte
	throttler.release(&msgMetadata{msgSize: 1}, vdr1ID)

	// Byte should have gone toward next validator message
	throttler.lock.Lock()
	require.Equal(2, throttler.waitingToAcquire.Len())
	require.Contains(throttler.nodeToWaitingMsgID, vdr1ID)
	firstMsgID := throttler.nodeToWaitingMsgID[vdr1ID]
	firstMsg, exists := throttler.waitingToAcquire.Get(firstMsgID)
	require.True(exists)
	require.Equal(maxBytes-2, firstMsg.bytesNeeded)
	throttler.lock.Unlock()

	select {
	case <-doneVdr:
		require.FailNow("should still be blocking")
	case <-time.After(50 * time.Millisecond):
	}

	// Release the rest of the bytes
	throttler.release(&msgMetadata{msgSize: maxBytes - 1}, vdr1ID)
	// next validator message should finish
	<-doneVdr
	throttler.release(&msgMetadata{msgSize: maxBytes - 1}, vdr1ID)
	// next non validator message should finish
	<-done
}
