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
		MaxUnprocessedVdrBytes:     1024,
		MaxUnprocessedAtLargeBytes: 1024,
		MaxNonVdrBytes:             1024,
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
	assert.Equal(config.MaxUnprocessedVdrBytes, throttler.maxUnprocessedVdrBytes)
	assert.Equal(config.MaxUnprocessedVdrBytes, throttler.remainingVdrBytes)
	assert.Equal(config.MaxUnprocessedAtLargeBytes, throttler.remainingAtLargeBytes)
	assert.NotNil(throttler.vdrToBytesUsed)
	assert.NotNil(throttler.log)
	assert.NotNil(throttler.vdrs)
	assert.NotNil(throttler.cond.L)
	assert.NotNil(throttler.metrics)

	// Take from at-large allocation.
	// Should return immediately.
	throttlerIntf.Acquire(1, vdr1ID)
	assert.EqualValues(config.MaxUnprocessedAtLargeBytes-1, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.MaxUnprocessedVdrBytes, throttler.remainingVdrBytes)
	assert.Len(throttler.vdrToBytesUsed, 0)

	// Release the bytes
	throttlerIntf.Release(1, vdr1ID)
	assert.EqualValues(config.MaxUnprocessedAtLargeBytes, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.MaxUnprocessedVdrBytes, throttler.remainingVdrBytes)
	assert.Len(throttler.vdrToBytesUsed, 0)

	// Use all the at-large allocation bytes and 1 of the validator allocation bytes
	// Should return immediately.
	throttlerIntf.Acquire(config.MaxUnprocessedAtLargeBytes+1, vdr1ID)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)
	assert.EqualValues(config.MaxUnprocessedVdrBytes-1, throttler.remainingVdrBytes)
	assert.EqualValues(throttler.vdrToBytesUsed[vdr1ID], 1)
	assert.Len(throttler.vdrToBytesUsed, 1)

	// The other validator should be able to acquire half the validator allocation.
	// Should return immediately.
	throttlerIntf.Acquire(config.MaxUnprocessedAtLargeBytes/2, vdr2ID)
	assert.EqualValues(config.MaxUnprocessedVdrBytes/2-1, throttler.remainingVdrBytes)
	assert.EqualValues(throttler.vdrToBytesUsed[vdr1ID], 1)
	assert.EqualValues(throttler.vdrToBytesUsed[vdr2ID], config.MaxUnprocessedVdrBytes/2)
	assert.Len(throttler.vdrToBytesUsed, 2)

	// vdr1 should be able to acquire the rest of the validator allocation
	// Should return immediately.
	throttlerIntf.Acquire(config.MaxUnprocessedAtLargeBytes/2-1, vdr1ID)
	assert.EqualValues(throttler.vdrToBytesUsed[vdr1ID], config.MaxUnprocessedVdrBytes/2)

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

	// Release config.MaxUnprocessedAtLargeBytes+1 bytes
	// When the choice exists, bytes should be given back to the validator allocation
	// rather than the at-large allocation
	// vdr1's validator allocation should now be unused and
	// the at-large allocation should have 510 bytes
	throttlerIntf.Release(config.MaxUnprocessedAtLargeBytes+1, vdr1ID)

	// The Acquires that blocked above should have returned
	<-vdr1Done
	<-vdr2Done
	<-nonVdrDone

	assert.EqualValues(config.MaxNonVdrBytes/2, throttler.remainingVdrBytes)
	assert.Len(throttler.vdrToBytesUsed, 1)
	assert.EqualValues(throttler.vdrToBytesUsed[vdr1ID], 0)
	assert.EqualValues(config.MaxUnprocessedAtLargeBytes/2-2, throttler.remainingAtLargeBytes)

	// Non-validator should be able to take the rest of the at-large bytes
	throttlerIntf.Acquire(config.MaxUnprocessedAtLargeBytes/2-2, nonVdrID)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)

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
	throttlerIntf.Release(config.MaxUnprocessedAtLargeBytes/2, vdr2ID)
	throttlerIntf.Release(1, vdr2ID)

	<-nonVdrDone

	assert.EqualValues(config.MaxUnprocessedVdrBytes, throttler.remainingVdrBytes)
	assert.Len(throttler.vdrToBytesUsed, 0)
	assert.EqualValues(0, throttler.remainingAtLargeBytes)

	// Release all of vdr1's messages
	throttlerIntf.Release(1, vdr1ID)
	throttlerIntf.Release(config.MaxUnprocessedAtLargeBytes/2-1, vdr1ID)
	assert.Len(throttler.vdrToBytesUsed, 0)
	assert.EqualValues(config.MaxUnprocessedVdrBytes, throttler.remainingVdrBytes)
	assert.EqualValues(config.MaxUnprocessedAtLargeBytes/2, throttler.remainingAtLargeBytes)

	// Release nonVdr's messages
	throttlerIntf.Release(1, nonVdrID)
	throttlerIntf.Release(1, nonVdrID)
	throttlerIntf.Release(config.MaxUnprocessedAtLargeBytes/2-2, nonVdrID)
	assert.Len(throttler.vdrToBytesUsed, 0)
	assert.EqualValues(config.MaxUnprocessedVdrBytes, throttler.remainingVdrBytes)
	assert.EqualValues(config.MaxUnprocessedAtLargeBytes, throttler.remainingAtLargeBytes)
}
