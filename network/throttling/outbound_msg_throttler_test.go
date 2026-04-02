// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestSybilOutboundMsgThrottler(t *testing.T) {
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
	throttlerIntf, err := NewSybilOutboundMsgThrottler(
		logging.NoLog{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	require.NoError(err)

	// Make sure NewSybilOutboundMsgThrottler works
	throttler := throttlerIntf.(*outboundMsgThrottler)
	require.Equal(config.VdrAllocSize, throttler.maxVdrBytes)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	require.NotNil(throttler.nodeToVdrBytesUsed)
	require.NotNil(throttler.log)
	require.NotNil(throttler.vdrs)

	// Take from at-large allocation.
	msg := testMsgWithSize(1)
	acquired := throttlerIntf.Acquire(msg, vdr1ID)
	require.True(acquired)
	require.Equal(config.AtLargeAllocSize-1, throttler.remainingAtLargeBytes)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1)
	require.Equal(uint64(1), throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Release the bytes
	throttlerIntf.Release(msg, vdr1ID)
	require.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Empty(throttler.nodeToAtLargeBytesUsed)

	// Use all the at-large allocation bytes and 1 of the validator allocation bytes
	msg = testMsgWithSize(config.AtLargeAllocSize + 1)
	acquired = throttlerIntf.Acquire(msg, vdr1ID)
	require.True(acquired)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 1
	require.Zero(throttler.remainingAtLargeBytes)
	require.Equal(throttler.remainingVdrBytes, config.VdrAllocSize-1)
	require.Equal(uint64(1), throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Len(throttler.nodeToVdrBytesUsed, 1)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1)
	require.Equal(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// The other validator should be able to acquire half the validator allocation.
	msg = testMsgWithSize(config.AtLargeAllocSize / 2)
	acquired = throttlerIntf.Acquire(msg, vdr2ID)
	require.True(acquired)
	// vdr2 at-large bytes used: 0. Validator bytes used: 512
	require.Equal(throttler.remainingVdrBytes, config.VdrAllocSize/2-1)
	require.Equal(uint64(1), throttler.nodeToVdrBytesUsed[vdr1ID], 1)
	require.Equal(config.VdrAllocSize/2, throttler.nodeToVdrBytesUsed[vdr2ID])
	require.Len(throttler.nodeToVdrBytesUsed, 2)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1)

	// vdr1 should be able to acquire the rest of the validator allocation
	msg = testMsgWithSize(config.VdrAllocSize/2 - 1)
	acquired = throttlerIntf.Acquire(msg, vdr1ID)
	require.True(acquired)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 512
	require.Equal(throttler.nodeToVdrBytesUsed[vdr1ID], config.VdrAllocSize/2)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1)
	require.Equal(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Trying to take more bytes for either node should fail
	msg = testMsgWithSize(1)
	acquired = throttlerIntf.Acquire(msg, vdr1ID)
	require.False(acquired)
	acquired = throttlerIntf.Acquire(msg, vdr2ID)
	require.False(acquired)
	// Should also fail for non-validators
	acquired = throttlerIntf.Acquire(msg, ids.GenerateTestNodeID())
	require.False(acquired)

	// Release config.MaxAtLargeBytes+1 (1025) bytes
	// When the choice exists, bytes should be given back to the validator allocation
	// rather than the at-large allocation.
	// vdr1 at-large bytes used: 511. Validator bytes used: 0
	msg = testMsgWithSize(config.AtLargeAllocSize + 1)
	throttlerIntf.Release(msg, vdr1ID)

	require.Equal(config.NodeMaxAtLargeBytes/2, throttler.remainingVdrBytes)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1) // vdr1
	require.Equal(config.AtLargeAllocSize/2-1, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	require.Len(throttler.nodeToVdrBytesUsed, 1)
	require.Equal(config.AtLargeAllocSize/2+1, throttler.remainingAtLargeBytes)

	// Non-validator should be able to take the rest of the at-large bytes
	// nonVdrID at-large bytes used: 513
	nonVdrID := ids.GenerateTestNodeID()
	msg = testMsgWithSize(config.AtLargeAllocSize/2 + 1)
	acquired = throttlerIntf.Acquire(msg, nonVdrID)
	require.True(acquired)
	require.Zero(throttler.remainingAtLargeBytes)
	require.Equal(config.AtLargeAllocSize/2+1, throttler.nodeToAtLargeBytesUsed[nonVdrID])

	// Non-validator shouldn't be able to acquire more since at-large allocation empty
	msg = testMsgWithSize(1)
	acquired = throttlerIntf.Acquire(msg, nonVdrID)
	require.False(acquired)

	// Release all of vdr2's messages
	msg = testMsgWithSize(config.AtLargeAllocSize / 2)
	throttlerIntf.Release(msg, vdr2ID)
	require.Zero(throttler.nodeToAtLargeBytesUsed[vdr2ID])
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Zero(throttler.remainingAtLargeBytes)

	// Release all of vdr1's messages
	msg = testMsgWithSize(config.VdrAllocSize/2 - 1)
	throttlerIntf.Release(msg, vdr1ID)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Equal(config.AtLargeAllocSize/2-1, throttler.remainingAtLargeBytes)
	require.Zero(throttler.nodeToAtLargeBytesUsed[vdr1ID])

	// Release nonVdr's messages
	msg = testMsgWithSize(config.AtLargeAllocSize/2 + 1)
	throttlerIntf.Release(msg, nonVdrID)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	require.Empty(throttler.nodeToAtLargeBytesUsed)
	require.Zero(throttler.nodeToAtLargeBytesUsed[nonVdrID])
}

// Ensure that the limit on taking from the at-large allocation is enforced
func TestSybilOutboundMsgThrottlerMaxNonVdr(t *testing.T) {
	require := require.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        100,
		AtLargeAllocSize:    100,
		NodeMaxAtLargeBytes: 10,
	}
	vdrs := validators.NewManager()
	vdr1ID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr1ID, nil, ids.Empty, 1))
	throttlerIntf, err := NewSybilOutboundMsgThrottler(
		logging.NoLog{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	require.NoError(err)
	throttler := throttlerIntf.(*outboundMsgThrottler)
	nonVdrNodeID1 := ids.GenerateTestNodeID()
	msg := testMsgWithSize(config.NodeMaxAtLargeBytes)
	acquired := throttlerIntf.Acquire(msg, nonVdrNodeID1)
	require.True(acquired)

	// Acquiring more should fail
	msg = testMsgWithSize(1)
	acquired = throttlerIntf.Acquire(msg, nonVdrNodeID1)
	require.False(acquired)

	// A different non-validator should be able to acquire
	nonVdrNodeID2 := ids.GenerateTestNodeID()
	msg = testMsgWithSize(config.NodeMaxAtLargeBytes)
	acquired = throttlerIntf.Acquire(msg, nonVdrNodeID2)
	require.True(acquired)

	// Validator should only be able to take [MaxAtLargeBytes]
	msg = testMsgWithSize(config.NodeMaxAtLargeBytes + 1)
	throttlerIntf.Acquire(msg, vdr1ID)
	require.Equal(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	require.Equal(uint64(1), throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Equal(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID1])
	require.Equal(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID2])
	require.Equal(config.AtLargeAllocSize-config.NodeMaxAtLargeBytes*3, throttler.remainingAtLargeBytes)
}

// Ensure that the throttler honors requested bypasses
func TestBypassThrottling(t *testing.T) {
	require := require.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        100,
		AtLargeAllocSize:    100,
		NodeMaxAtLargeBytes: 10,
	}
	vdrs := validators.NewManager()
	vdr1ID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr1ID, nil, ids.Empty, 1))
	throttlerIntf, err := NewSybilOutboundMsgThrottler(
		logging.NoLog{},
		prometheus.NewRegistry(),
		vdrs,
		config,
	)
	require.NoError(err)
	throttler := throttlerIntf.(*outboundMsgThrottler)
	nonVdrNodeID1 := ids.GenerateTestNodeID()
	msg := &message.OutboundMessage{
		BypassThrottling: true,
		Op:               message.AppGossipOp,
		Bytes:            make([]byte, config.NodeMaxAtLargeBytes),
	}
	acquired := throttlerIntf.Acquire(msg, nonVdrNodeID1)
	require.True(acquired)

	// Acquiring more should not fail
	msg = &message.OutboundMessage{
		BypassThrottling: true,
		Op:               message.AppGossipOp,
		Bytes:            make([]byte, 1),
	}
	acquired = throttlerIntf.Acquire(msg, nonVdrNodeID1)
	require.True(acquired)

	// Acquiring more should not fail
	msg2 := testMsgWithSize(1)
	acquired = throttlerIntf.Acquire(msg2, nonVdrNodeID1)
	require.True(acquired)

	// Validator should only be able to take [MaxAtLargeBytes]
	msg = &message.OutboundMessage{
		BypassThrottling: true,
		Op:               message.AppGossipOp,
		Bytes:            make([]byte, config.NodeMaxAtLargeBytes+1),
	}
	throttlerIntf.Acquire(msg, vdr1ID)
	require.Zero(throttler.nodeToAtLargeBytesUsed[vdr1ID])
	require.Zero(throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Equal(uint64(1), throttler.nodeToAtLargeBytesUsed[nonVdrNodeID1])
	require.Equal(config.AtLargeAllocSize-1, throttler.remainingAtLargeBytes)
}

func testMsgWithSize(size uint64) *message.OutboundMessage {
	return &message.OutboundMessage{
		BypassThrottling: false,
		Op:               message.AppGossipOp,
		Bytes:            make([]byte, size),
	}
}
