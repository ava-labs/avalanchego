// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestInboundMsgByteThrottlerCancelContextDeadlock(t *testing.T) {
	assert := assert.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1,
		AtLargeAllocSize:    1,
		NodeMaxAtLargeBytes: 1,
	}
	vdrs := validators.NewSet()
	vdr := ids.GenerateTestNodeID()
	assert.NoError(vdrs.AddWeight(vdr, 1))

	throttler, err := newInboundMsgByteThrottler(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	assert.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	nodeID := ids.GenerateTestNodeID()
	release := throttler.Acquire(ctx, 2, nodeID)
	release()
}

func TestInboundMsgByteThrottlerCancelContext(t *testing.T) {
	assert := assert.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1024,
		AtLargeAllocSize:    512,
		NodeMaxAtLargeBytes: 1024,
	}
	vdrs := validators.NewSet()
	vdr1ID := ids.GenerateTestNodeID()
	vdr2ID := ids.GenerateTestNodeID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	assert.NoError(vdrs.AddWeight(vdr2ID, 1))

	throttler, err := newInboundMsgByteThrottler(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	assert.NoError(err)

	throttler.Acquire(context.Background(), config.VdrAllocSize, vdr1ID)

	// Trying to take more bytes for node should block
	vdr2Done := make(chan struct{})
	vdr2Context, vdr2ContextCancelFunction := context.WithCancel(context.Background())
	go func() {
		throttler.Acquire(vdr2Context, config.VdrAllocSize, vdr2ID)
		vdr2Done <- struct{}{}
	}()
	select {
	case <-vdr2Done:
		t.Fatal("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// ensure the throttler has recorded that vdr2 is waiting
	throttler.lock.Lock()
	assert.Len(throttler.nodeToWaitingMsgIDs, 1)
	assert.Len(throttler.nodeToWaitingMsgIDs[vdr2ID], 1)
	assert.EqualValues(1, throttler.waitingToAcquire.Len())
	_, exists := throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgIDs[vdr2ID][0])
	assert.True(exists)
	throttler.lock.Unlock()

	// cancel should cause vdr2's acquire to unblock
	vdr2ContextCancelFunction()

	select {
	case <-vdr2Done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("channel should signal because ctx was cancelled")
	}
}

func TestInboundMsgByteThrottler(t *testing.T) {
	assert := assert.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1024,
		AtLargeAllocSize:    1024,
		NodeMaxAtLargeBytes: 1024,
	}
	vdrs := validators.NewSet()
	vdr1ID := ids.GenerateTestNodeID()
	vdr2ID := ids.GenerateTestNodeID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	assert.NoError(vdrs.AddWeight(vdr2ID, 1))

	throttler, err := newInboundMsgByteThrottler(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	assert.NoError(err)

	// Make sure NewSybilInboundMsgThrottler works
	assert.Equal(config.VdrAllocSize, throttler.maxVdrBytes)
	assert.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	assert.NotNil(throttler.nodeToVdrBytesUsed)
	assert.NotNil(throttler.log)
	assert.NotNil(throttler.vdrs)
	assert.NotNil(throttler.metrics)

	// Take from at-large allocation.
	// Should return immediately.
	throttler.Acquire(context.Background(), 1, vdr1ID)
	assert.EqualValues(config.AtLargeAllocSize-1, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)
	assert.EqualValues(1, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Release the bytes
	throttler.release(&msgMetadata{msgSize: 1}, vdr1ID)
	assert.EqualValues(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 0)

	// Use all the at-large allocation bytes and 1 of the validator allocation bytes
	// Should return immediately.
	throttler.Acquire(context.Background(), config.AtLargeAllocSize+1, vdr1ID)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 1
	assert.EqualValues(0, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.VdrAllocSize-1, throttler.remainingVdrBytes)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], 1)
	assert.Len(throttler.nodeToVdrBytesUsed, 1)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)
	assert.EqualValues(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// The other validator should be able to acquire half the validator allocation.
	// Should return immediately.
	throttler.Acquire(context.Background(), config.AtLargeAllocSize/2, vdr2ID)
	// vdr2 at-large bytes used: 0. Validator bytes used: 512
	assert.EqualValues(config.VdrAllocSize/2-1, throttler.remainingVdrBytes)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], 1)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr2ID], config.VdrAllocSize/2)
	assert.Len(throttler.nodeToVdrBytesUsed, 2)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)
	assert.Len(throttler.nodeToWaitingMsgIDs, 0)
	assert.EqualValues(0, throttler.waitingToAcquire.Len())

	// vdr1 should be able to acquire the rest of the validator allocation
	// Should return immediately.
	throttler.Acquire(context.Background(), config.VdrAllocSize/2-1, vdr1ID)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 512
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], config.VdrAllocSize/2)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)
	assert.EqualValues(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Trying to take more bytes for either node should block
	vdr1Done := make(chan struct{})
	go func() {
		throttler.Acquire(context.Background(), 1, vdr1ID)
		vdr1Done <- struct{}{}
	}()
	select {
	case <-vdr1Done:
		t.Fatal("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}
	throttler.lock.Lock()
	assert.Len(throttler.nodeToWaitingMsgIDs, 1)
	assert.Len(throttler.nodeToWaitingMsgIDs[vdr1ID], 1)
	assert.EqualValues(1, throttler.waitingToAcquire.Len())
	_, exists := throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgIDs[vdr1ID][0])
	assert.True(exists)
	throttler.lock.Unlock()

	vdr2Done := make(chan struct{})
	go func() {
		throttler.Acquire(context.Background(), 1, vdr2ID)
		vdr2Done <- struct{}{}
	}()
	select {
	case <-vdr2Done:
		t.Fatal("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}
	throttler.lock.Lock()
	assert.Len(throttler.nodeToWaitingMsgIDs, 2)
	assert.Len(throttler.nodeToWaitingMsgIDs[vdr2ID], 1)
	assert.EqualValues(2, throttler.waitingToAcquire.Len())
	_, exists = throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgIDs[vdr2ID][0])
	assert.True(exists)
	throttler.lock.Unlock()

	nonVdrID := ids.GenerateTestNodeID()
	nonVdrDone := make(chan struct{})
	go func() {
		throttler.Acquire(context.Background(), 1, nonVdrID)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		t.Fatal("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}
	throttler.lock.Lock()
	assert.Len(throttler.nodeToWaitingMsgIDs, 3)
	assert.Len(throttler.nodeToWaitingMsgIDs[nonVdrID], 1)
	assert.EqualValues(3, throttler.waitingToAcquire.Len())
	_, exists = throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgIDs[nonVdrID][0])
	assert.True(exists)
	throttler.lock.Unlock()

	// Release config.MaxAtLargeBytes+1 bytes
	// When the choice exists, bytes should be given back to the validator allocation
	// rather than the at-large allocation.
	throttler.release(&msgMetadata{msgSize: config.AtLargeAllocSize + 1}, vdr1ID)

	// The Acquires that blocked above should have returned
	<-vdr1Done
	<-vdr2Done
	<-nonVdrDone

	assert.EqualValues(config.NodeMaxAtLargeBytes/2, throttler.remainingVdrBytes)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 3) // vdr1, vdr2, nonVdrID
	assert.EqualValues(config.AtLargeAllocSize/2, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	assert.EqualValues(1, throttler.nodeToAtLargeBytesUsed[vdr2ID])
	assert.EqualValues(1, throttler.nodeToAtLargeBytesUsed[nonVdrID])
	assert.Len(throttler.nodeToVdrBytesUsed, 1)
	assert.EqualValues(0, throttler.nodeToVdrBytesUsed[vdr1ID])
	assert.EqualValues(config.AtLargeAllocSize/2-2, throttler.remainingAtLargeBytes)
	assert.Len(throttler.nodeToWaitingMsgIDs, 0)
	assert.EqualValues(0, throttler.waitingToAcquire.Len())

	// Non-validator should be able to take the rest of the at-large bytes
	throttler.Acquire(context.Background(), config.AtLargeAllocSize/2-2, nonVdrID)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.AtLargeAllocSize/2-1, throttler.nodeToAtLargeBytesUsed[nonVdrID])
	assert.Len(throttler.nodeToWaitingMsgIDs, 0)
	assert.EqualValues(0, throttler.waitingToAcquire.Len())

	// But should block on subsequent Acquires
	go func() {
		throttler.Acquire(context.Background(), 1, nonVdrID)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		t.Fatal("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}
	throttler.lock.Lock()
	assert.Len(throttler.nodeToWaitingMsgIDs, 1)
	assert.Len(throttler.nodeToWaitingMsgIDs[nonVdrID], 1)
	assert.EqualValues(1, throttler.waitingToAcquire.Len())
	_, exists = throttler.waitingToAcquire.Get(throttler.nodeToWaitingMsgIDs[nonVdrID][0])
	assert.True(exists)
	throttler.lock.Unlock()

	// Release all of vdr2's messages
	throttler.release(&msgMetadata{msgSize: config.AtLargeAllocSize / 2}, vdr2ID)
	throttler.release(&msgMetadata{msgSize: 1}, vdr2ID)

	<-nonVdrDone

	assert.EqualValues(0, throttler.nodeToAtLargeBytesUsed[vdr2ID])
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)
	assert.Len(throttler.nodeToWaitingMsgIDs, 0)
	assert.EqualValues(0, throttler.waitingToAcquire.Len())

	// Release all of vdr1's messages
	throttler.release(&msgMetadata{msgSize: 1}, vdr1ID)
	throttler.release(&msgMetadata{msgSize: config.AtLargeAllocSize/2 - 1}, vdr1ID)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.EqualValues(config.AtLargeAllocSize/2, throttler.remainingAtLargeBytes)
	assert.EqualValues(0, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	assert.Len(throttler.nodeToWaitingMsgIDs, 0)
	assert.EqualValues(0, throttler.waitingToAcquire.Len())

	// Release nonVdr's messages
	throttler.release(&msgMetadata{msgSize: 1}, nonVdrID)
	throttler.release(&msgMetadata{msgSize: 1}, nonVdrID)
	throttler.release(&msgMetadata{msgSize: config.AtLargeAllocSize/2 - 2}, nonVdrID)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.EqualValues(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 0)
	assert.EqualValues(0, throttler.nodeToAtLargeBytesUsed[nonVdrID])
	assert.Len(throttler.nodeToWaitingMsgIDs, 0)
	assert.EqualValues(0, throttler.waitingToAcquire.Len())
}

// Ensure that the limit on taking from the at-large allocation is enforced
func TestSybilMsgThrottlerMaxNonVdr(t *testing.T) {
	assert := assert.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        100,
		AtLargeAllocSize:    100,
		NodeMaxAtLargeBytes: 10,
	}
	vdrs := validators.NewSet()
	vdr1ID := ids.GenerateTestNodeID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	throttler, err := newInboundMsgByteThrottler(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	assert.NoError(err)
	nonVdrNodeID1 := ids.GenerateTestNodeID()
	throttler.Acquire(context.Background(), config.NodeMaxAtLargeBytes, nonVdrNodeID1)

	// Acquiring more should block
	nonVdrDone := make(chan struct{})
	go func() {
		throttler.Acquire(context.Background(), 1, nonVdrNodeID1)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		t.Fatal("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// A different non-validator should be able to acquire
	nonVdrNodeID2 := ids.GenerateTestNodeID()
	throttler.Acquire(context.Background(), config.NodeMaxAtLargeBytes, nonVdrNodeID2)

	// Acquiring more should block
	go func() {
		throttler.Acquire(context.Background(), 1, nonVdrNodeID1)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		t.Fatal("should block on acquiring any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// Validator should only be able to take [MaxAtLargeBytes]
	throttler.Acquire(context.Background(), config.NodeMaxAtLargeBytes+1, vdr1ID)
	assert.EqualValues(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	assert.EqualValues(1, throttler.nodeToVdrBytesUsed[vdr1ID])
	assert.EqualValues(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID1])
	assert.EqualValues(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID2])
	assert.EqualValues(config.AtLargeAllocSize-config.NodeMaxAtLargeBytes*3, throttler.remainingAtLargeBytes)
}

// Test that messages waiting to be acquired by a given node
// are handled in FIFO order
func TestSybilMsgThrottlerFIFO(t *testing.T) {
	assert := assert.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1024,
		AtLargeAllocSize:    1024,
		NodeMaxAtLargeBytes: 1024,
	}
	vdrs := validators.NewSet()
	vdr1ID := ids.GenerateTestNodeID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	nonVdrNodeID := ids.GenerateTestNodeID()

	maxVdrBytes := config.VdrAllocSize + config.AtLargeAllocSize
	maxNonVdrBytes := config.AtLargeAllocSize
	// Test for both validator and non-validator
	for _, nodeID := range []ids.NodeID{vdr1ID, nonVdrNodeID} {
		maxBytes := maxVdrBytes
		if nodeID == nonVdrNodeID {
			maxBytes = maxNonVdrBytes
		}
		throttler, err := newInboundMsgByteThrottler(
			logging.NoLog{},
			"",
			prometheus.NewRegistry(),
			vdrs,
			config,
		)
		assert.NoError(err)
		// node uses up all but 1 byte
		throttler.Acquire(context.Background(), maxBytes-1, nodeID)
		// node uses the last byte
		throttler.Acquire(context.Background(), 1, nodeID)

		// First message wants to acquire a lot of bytes
		done := make(chan struct{})
		go func() {
			throttler.Acquire(context.Background(), maxBytes-1, nodeID)
			done <- struct{}{}
		}()
		select {
		case <-done:
			t.Fatal("should block on acquiring any more bytes")
		case <-time.After(50 * time.Millisecond):
		}

		// Next message only wants to acquire 1 byte
		go func() {
			throttler.Acquire(context.Background(), 1, nodeID)
			done <- struct{}{}
		}()
		select {
		case <-done:
			t.Fatal("should block on acquiring any more bytes")
		case <-time.After(50 * time.Millisecond):
		}

		// Release 1 byte
		throttler.release(&msgMetadata{msgSize: 1}, nodeID)
		// Byte should have gone toward first message
		assert.EqualValues(2, throttler.waitingToAcquire.Len())
		assert.Len(throttler.nodeToWaitingMsgIDs[nodeID], 2)
		firstMsgID := throttler.nodeToWaitingMsgIDs[nodeID][0]
		firstMsg, exists := throttler.waitingToAcquire.Get(firstMsgID)
		assert.True(exists)
		assert.EqualValues(maxBytes-2, firstMsg.(*msgMetadata).bytesNeeded)

		// Since messages are processed FIFO for a given validator,
		// the first message should return from Acquire first
		select {
		case <-done:
			t.Fatal("should still be blocking")
		case <-time.After(50 * time.Millisecond):
		}

		// Release the rest of the bytes
		throttler.release(&msgMetadata{msgSize: maxBytes - 1}, nodeID)
		// Both should be done acquiring now
		<-done
		<-done
	}
}
