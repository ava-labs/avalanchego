// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestSybilMsgThrottler(t *testing.T) {
	assert := assert.New(t)
	config := MsgThrottlerConfig{
		VdrAllocSize:        1024,
		AtLargeAllocSize:    1024,
		NodeMaxAtLargeBytes: 1024,
	}
	vdrs := validators.NewSet()
	vdr1ID := ids.GenerateTestShortID()
	vdr2ID := ids.GenerateTestShortID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	assert.NoError(vdrs.AddWeight(vdr2ID, 1))
	throttlerIntf, err := NewSybilMsgThrottler(
		&logging.Log{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	assert.NoError(err)

	// Make sure NewSybilMsgThrottler works
	throttler := throttlerIntf.(*sybilMsgThrottler)
	assert.Equal(config.VdrAllocSize, throttler.maxVdrBytes)
	assert.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	assert.NotNil(throttler.nodeToVdrBytesUsed)
	assert.NotNil(throttler.log)
	assert.NotNil(throttler.vdrs)
	assert.NotNil(throttler.cond.L)
	assert.NotNil(throttler.metrics)

	// Take from at-large allocation.
	// Should return immediately.
	throttlerIntf.Acquire(1, vdr1ID)
	assert.EqualValues(config.AtLargeAllocSize-1, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)
	assert.EqualValues(1, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Release the bytes
	throttlerIntf.Release(1, vdr1ID)
	assert.EqualValues(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 0)

	// Use all the at-large allocation bytes and 1 of the validator allocation bytes
	// Should return immediately.
	throttlerIntf.Acquire(config.AtLargeAllocSize+1, vdr1ID)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.VdrAllocSize-1, throttler.remainingVdrBytes)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], 1)
	assert.Len(throttler.nodeToVdrBytesUsed, 1)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)
	assert.EqualValues(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// The other validator should be able to acquire half the validator allocation.
	// Should return immediately.
	throttlerIntf.Acquire(config.AtLargeAllocSize/2, vdr2ID)
	assert.EqualValues(config.VdrAllocSize/2-1, throttler.remainingVdrBytes)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], 1)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr2ID], config.VdrAllocSize/2)
	assert.Len(throttler.nodeToVdrBytesUsed, 2)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)

	// vdr1 should be able to acquire the rest of the validator allocation
	// Should return immediately.
	throttlerIntf.Acquire(config.AtLargeAllocSize/2-1, vdr1ID)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], config.VdrAllocSize/2)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)
	assert.EqualValues(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Trying to take more bytes for either node should block
	vdr1Done := make(chan struct{})
	go func() {
		throttlerIntf.Acquire(1, vdr1ID)
		vdr1Done <- struct{}{}
	}()
	select {
	case <-vdr1Done:
		t.Fatal("should block on fetching any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	vdr2Done := make(chan struct{})
	go func() {
		throttlerIntf.Acquire(1, vdr2ID)
		vdr2Done <- struct{}{}
	}()
	select {
	case <-vdr2Done:
		t.Fatal("should block on fetching any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	nonVdrID := ids.GenerateTestShortID()
	nonVdrDone := make(chan struct{})
	go func() {
		throttlerIntf.Acquire(1, nonVdrID)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		t.Fatal("should block on fetching any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// Release config.MaxAtLargeBytes+1 bytes
	// When the choice exists, bytes should be given back to the validator allocation
	// rather than the at-large allocation
	// vdr1's validator allocation should now be unused and
	// the at-large allocation should have 510 bytes
	throttlerIntf.Release(config.AtLargeAllocSize+1, vdr1ID)

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
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], 0)
	assert.EqualValues(config.AtLargeAllocSize/2-2, throttler.remainingAtLargeBytes)

	// Non-validator should be able to take the rest of the at-large bytes
	throttlerIntf.Acquire(config.AtLargeAllocSize/2-2, nonVdrID)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.AtLargeAllocSize/2-1, throttler.nodeToAtLargeBytesUsed[nonVdrID])

	// But should block on subsequent Acquires
	go func() {
		throttlerIntf.Acquire(1, nonVdrID)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		t.Fatal("should block on fetching any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// Release all of vdr2's messages
	throttlerIntf.Release(config.AtLargeAllocSize/2, vdr2ID)
	throttlerIntf.Release(1, vdr2ID)

	<-nonVdrDone

	assert.EqualValues(0, throttler.nodeToAtLargeBytesUsed[vdr2ID])
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)

	// Release all of vdr1's messages
	throttlerIntf.Release(1, vdr1ID)
	throttlerIntf.Release(config.AtLargeAllocSize/2-1, vdr1ID)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.EqualValues(config.AtLargeAllocSize/2, throttler.remainingAtLargeBytes)
	assert.EqualValues(0, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Release nonVdr's messages
	throttlerIntf.Release(1, nonVdrID)
	throttlerIntf.Release(1, nonVdrID)
	throttlerIntf.Release(config.AtLargeAllocSize/2-2, nonVdrID)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.EqualValues(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 0)
	assert.EqualValues(0, throttler.nodeToAtLargeBytesUsed[nonVdrID])
}

func TestSybilMsgThrottlerMaxNonVdr(t *testing.T) {
	assert := assert.New(t)
	config := MsgThrottlerConfig{
		VdrAllocSize:        100,
		AtLargeAllocSize:    100,
		NodeMaxAtLargeBytes: 10,
	}
	vdrs := validators.NewSet()
	vdr1ID := ids.GenerateTestShortID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	throttlerIntf, err := NewSybilMsgThrottler(
		&logging.Log{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	assert.NoError(err)
	throttler := throttlerIntf.(*sybilMsgThrottler)
	nonVdrNodeID1 := ids.GenerateTestShortID()
	throttlerIntf.Acquire(config.NodeMaxAtLargeBytes, nonVdrNodeID1)

	// Acquiring more should block
	nonVdrDone := make(chan struct{})
	go func() {
		throttlerIntf.Acquire(1, nonVdrNodeID1)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		t.Fatal("should block on fetching any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// A different non-validator should be able to acquire
	nonVdrNodeID2 := ids.GenerateTestShortID()
	throttlerIntf.Acquire(config.NodeMaxAtLargeBytes, nonVdrNodeID2)

	// Acquiring more should block
	go func() {
		throttlerIntf.Acquire(1, nonVdrNodeID1)
		nonVdrDone <- struct{}{}
	}()
	select {
	case <-nonVdrDone:
		t.Fatal("should block on fetching any more bytes")
	case <-time.After(50 * time.Millisecond):
	}

	// Validator should only be able to take [MaxAtLargeBytes]
	throttlerIntf.Acquire(config.NodeMaxAtLargeBytes+1, vdr1ID)
	assert.EqualValues(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	assert.EqualValues(1, throttler.nodeToVdrBytesUsed[vdr1ID])
	assert.EqualValues(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID1])
	assert.EqualValues(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID2])
	assert.EqualValues(config.AtLargeAllocSize-config.NodeMaxAtLargeBytes*3, throttler.remainingAtLargeBytes)
}
