// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestSybilOutboundMsgThrottler(t *testing.T) {
	assert := assert.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1024,
		AtLargeAllocSize:    1024,
		NodeMaxAtLargeBytes: 1024,
	}
	vdrs := validators.NewSet()
	vdr1ID := ids.GenerateTestShortID()
	vdr2ID := ids.GenerateTestShortID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	assert.NoError(vdrs.AddWeight(vdr2ID, 1))
	throttlerIntf, err := NewSybilOutboundMsgThrottler(
		&logging.Log{},
		"",
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	assert.NoError(err)

	// Make sure NewSybilOutboundMsgThrottler works
	throttler := throttlerIntf.(*outboundMsgThrottler)
	assert.Equal(config.VdrAllocSize, throttler.maxVdrBytes)
	assert.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	assert.NotNil(throttler.nodeToVdrBytesUsed)
	assert.NotNil(throttler.log)
	assert.NotNil(throttler.vdrs)

	// Take from at-large allocation.
	acquired := throttlerIntf.Acquire(1, vdr1ID)
	assert.True(acquired)
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
	acquired = throttlerIntf.Acquire(config.AtLargeAllocSize+1, vdr1ID)
	assert.True(acquired)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 1
	assert.EqualValues(0, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.VdrAllocSize-1, throttler.remainingVdrBytes)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], 1)
	assert.Len(throttler.nodeToVdrBytesUsed, 1)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)
	assert.EqualValues(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// The other validator should be able to acquire half the validator allocation.
	acquired = throttlerIntf.Acquire(config.AtLargeAllocSize/2, vdr2ID)
	assert.True(acquired)
	// vdr2 at-large bytes used: 0. Validator bytes used: 512
	assert.EqualValues(config.VdrAllocSize/2-1, throttler.remainingVdrBytes)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], 1)
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr2ID], config.VdrAllocSize/2)
	assert.Len(throttler.nodeToVdrBytesUsed, 2)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)

	// vdr1 should be able to acquire the rest of the validator allocation
	acquired = throttlerIntf.Acquire(config.VdrAllocSize/2-1, vdr1ID)
	assert.True(acquired)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 512
	assert.EqualValues(throttler.nodeToVdrBytesUsed[vdr1ID], config.VdrAllocSize/2)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1)
	assert.EqualValues(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Trying to take more bytes for either node should fail
	acquired = throttlerIntf.Acquire(1, vdr1ID)
	assert.False(acquired)
	acquired = throttlerIntf.Acquire(1, vdr2ID)
	assert.False(acquired)
	// Should also fail for non-validators
	acquired = throttlerIntf.Acquire(1, ids.GenerateTestShortID())
	assert.False(acquired)

	// Release config.MaxAtLargeBytes+1 (1025) bytes
	// When the choice exists, bytes should be given back to the validator allocation
	// rather than the at-large allocation.
	// vdr1 at-large bytes used: 511. Validator bytes used: 0
	throttlerIntf.Release(config.AtLargeAllocSize+1, vdr1ID)

	assert.EqualValues(config.NodeMaxAtLargeBytes/2, throttler.remainingVdrBytes)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 1) // vdr1
	assert.EqualValues(config.AtLargeAllocSize/2-1, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	assert.Len(throttler.nodeToVdrBytesUsed, 1)
	assert.EqualValues(config.AtLargeAllocSize/2+1, throttler.remainingAtLargeBytes)

	// Non-validator should be able to take the rest of the at-large bytes
	// nonVdrID at-large bytes used: 513
	nonVdrID := ids.GenerateTestShortID()
	acquired = throttlerIntf.Acquire(config.AtLargeAllocSize/2+1, nonVdrID)
	assert.True(acquired)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.AtLargeAllocSize/2+1, throttler.nodeToAtLargeBytesUsed[nonVdrID])

	// Non-validator shouldn't be able to acquire more since at-large allocation empty
	acquired = throttlerIntf.Acquire(1, nonVdrID)
	assert.False(acquired)

	// Release all of vdr2's messages
	throttlerIntf.Release(config.AtLargeAllocSize/2, vdr2ID)
	assert.EqualValues(0, throttler.nodeToAtLargeBytesUsed[vdr2ID])
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)

	// Release all of vdr1's messages
	throttlerIntf.Release(config.VdrAllocSize/2-1, vdr1ID)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.EqualValues(config.AtLargeAllocSize/2-1, throttler.remainingAtLargeBytes)
	assert.EqualValues(0, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Release nonVdr's messages
	throttlerIntf.Release(config.AtLargeAllocSize/2+1, nonVdrID)
	assert.Len(throttler.nodeToVdrBytesUsed, 0)
	assert.EqualValues(config.VdrAllocSize, throttler.remainingVdrBytes)
	assert.EqualValues(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	assert.Len(throttler.nodeToAtLargeBytesUsed, 0)
	assert.EqualValues(0, throttler.nodeToAtLargeBytesUsed[nonVdrID])
}

// Ensure that the limit on taking from the at-large allocation is enforced
func TestSybilOutboundMsgThrottlerMaxNonVdr(t *testing.T) {
	assert := assert.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        100,
		AtLargeAllocSize:    100,
		NodeMaxAtLargeBytes: 10,
	}
	vdrs := validators.NewSet()
	vdr1ID := ids.GenerateTestShortID()
	assert.NoError(vdrs.AddWeight(vdr1ID, 1))
	throttlerIntf, err := NewSybilOutboundMsgThrottler(
		&logging.Log{},
		"",
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	assert.NoError(err)
	throttler := throttlerIntf.(*outboundMsgThrottler)
	nonVdrNodeID1 := ids.GenerateTestShortID()
	acquired := throttlerIntf.Acquire(config.NodeMaxAtLargeBytes, nonVdrNodeID1)
	assert.True(acquired)

	// Acquiring more should fail
	acquired = throttlerIntf.Acquire(1, nonVdrNodeID1)
	assert.False(acquired)

	// A different non-validator should be able to acquire
	nonVdrNodeID2 := ids.GenerateTestShortID()
	acquired = throttlerIntf.Acquire(config.NodeMaxAtLargeBytes, nonVdrNodeID2)
	assert.True(acquired)

	// Validator should only be able to take [MaxAtLargeBytes]
	throttlerIntf.Acquire(config.NodeMaxAtLargeBytes+1, vdr1ID)
	assert.EqualValues(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	assert.EqualValues(1, throttler.nodeToVdrBytesUsed[vdr1ID])
	assert.EqualValues(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID1])
	assert.EqualValues(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID2])
	assert.EqualValues(config.AtLargeAllocSize-config.NodeMaxAtLargeBytes*3, throttler.remainingAtLargeBytes)
}
