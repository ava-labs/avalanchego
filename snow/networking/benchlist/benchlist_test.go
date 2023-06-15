// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var minimumFailingDuration = 5 * time.Minute

// Test that validators are properly added to the bench
func TestBenchlistAdd(t *testing.T) {
	require := require.New(t)

	vdrs := validators.NewSet()
	vdrID0 := ids.GenerateTestNodeID()
	vdrID1 := ids.GenerateTestNodeID()
	vdrID2 := ids.GenerateTestNodeID()
	vdrID3 := ids.GenerateTestNodeID()
	vdrID4 := ids.GenerateTestNodeID()

	require.NoError(vdrs.Add(vdrID0, nil, ids.Empty, 50))
	require.NoError(vdrs.Add(vdrID1, nil, ids.Empty, 50))
	require.NoError(vdrs.Add(vdrID2, nil, ids.Empty, 50))
	require.NoError(vdrs.Add(vdrID3, nil, ids.Empty, 50))
	require.NoError(vdrs.Add(vdrID4, nil, ids.Empty, 50))

	benchable := &TestBenchable{T: t}
	benchable.Default(true)

	threshold := 3
	duration := time.Minute
	maxPortion := 0.5
	benchIntf, err := NewBenchlist(
		ids.Empty,
		logging.NoLog{},
		benchable,
		vdrs,
		threshold,
		minimumFailingDuration,
		duration,
		maxPortion,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	b := benchIntf.(*benchlist)
	defer b.timer.Stop()
	now := time.Now()
	b.clock.Set(now)

	// Nobody should be benched at the start
	b.lock.Lock()
	require.False(b.isBenched(vdrID0))
	require.False(b.isBenched(vdrID1))
	require.False(b.isBenched(vdrID2))
	require.False(b.isBenched(vdrID3))
	require.False(b.isBenched(vdrID4))
	require.Empty(b.failureStreaks)
	require.Empty(b.benchedQueue)
	require.Empty(b.benchlistSet)
	b.lock.Unlock()

	// Register [threshold - 1] failures in a row for vdr0
	for i := 0; i < threshold-1; i++ {
		b.RegisterFailure(vdrID0)
	}

	// Still shouldn't be benched due to not enough consecutive failure
	require.False(b.isBenched(vdrID0))
	require.Empty(b.benchedQueue)
	require.Empty(b.benchlistSet)
	require.Len(b.failureStreaks, 1)
	fs := b.failureStreaks[vdrID0]
	require.Equal(threshold-1, fs.consecutive)
	require.True(fs.firstFailure.Equal(now))

	// Register another failure
	b.RegisterFailure(vdrID0)

	// Still shouldn't be benched because not enough time (any in this case)
	// has passed since the first failure
	b.lock.Lock()
	require.False(b.isBenched(vdrID0))
	require.Empty(b.benchedQueue)
	require.Empty(b.benchlistSet)
	b.lock.Unlock()

	// Move the time up
	now = now.Add(minimumFailingDuration).Add(time.Second)
	b.lock.Lock()
	b.clock.Set(now)

	benched := false
	benchable.BenchedF = func(ids.ID, ids.NodeID) {
		benched = true
	}
	b.lock.Unlock()

	// Register another failure
	b.RegisterFailure(vdrID0)

	// Now this validator should be benched
	b.lock.Lock()
	require.True(b.isBenched(vdrID0))
	require.Equal(b.benchedQueue.Len(), 1)
	require.Equal(b.benchlistSet.Len(), 1)

	next := b.benchedQueue[0]
	require.Equal(vdrID0, next.nodeID)
	require.False(next.benchedUntil.After(now.Add(duration)))
	require.False(next.benchedUntil.Before(now.Add(duration / 2)))
	require.Empty(b.failureStreaks)
	require.True(benched)
	benchable.BenchedF = nil
	b.lock.Unlock()

	// Give another validator [threshold-1] failures
	for i := 0; i < threshold-1; i++ {
		b.RegisterFailure(vdrID1)
	}

	// Register another failure
	b.RegisterResponse(vdrID1)

	// vdr1 shouldn't be benched
	// The response should have cleared its consecutive failures
	b.lock.Lock()
	require.True(b.isBenched(vdrID0))
	require.False(b.isBenched(vdrID1))
	require.Equal(b.benchedQueue.Len(), 1)
	require.Equal(b.benchlistSet.Len(), 1)
	require.Empty(b.failureStreaks)
	b.lock.Unlock()

	// Register another failure for vdr0, who is benched
	b.RegisterFailure(vdrID0)

	// A failure for an already benched validator should not count against it
	b.lock.Lock()
	require.Empty(b.failureStreaks)
	b.lock.Unlock()
}

// Test that the benchlist won't bench more than the maximum portion of stake
func TestBenchlistMaxStake(t *testing.T) {
	require := require.New(t)

	vdrs := validators.NewSet()
	vdrID0 := ids.GenerateTestNodeID()
	vdrID1 := ids.GenerateTestNodeID()
	vdrID2 := ids.GenerateTestNodeID()
	vdrID3 := ids.GenerateTestNodeID()
	vdrID4 := ids.GenerateTestNodeID()

	// Total weight is 5100
	require.NoError(vdrs.Add(vdrID0, nil, ids.Empty, 1000))
	require.NoError(vdrs.Add(vdrID1, nil, ids.Empty, 1000))
	require.NoError(vdrs.Add(vdrID2, nil, ids.Empty, 1000))
	require.NoError(vdrs.Add(vdrID3, nil, ids.Empty, 2000))
	require.NoError(vdrs.Add(vdrID4, nil, ids.Empty, 100))

	threshold := 3
	duration := 1 * time.Hour
	// Shouldn't bench more than 2550 (5100/2)
	maxPortion := 0.5
	benchIntf, err := NewBenchlist(
		ids.Empty,
		logging.NoLog{},
		&TestBenchable{T: t},
		vdrs,
		threshold,
		minimumFailingDuration,
		duration,
		maxPortion,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	b := benchIntf.(*benchlist)
	defer b.timer.Stop()
	now := time.Now()
	b.clock.Set(now)

	// Register [threshold-1] failures for 3 validators
	for _, vdrID := range []ids.NodeID{vdrID0, vdrID1, vdrID2} {
		for i := 0; i < threshold-1; i++ {
			b.RegisterFailure(vdrID)
		}
	}

	// Advance the time to past the minimum failing duration
	newTime := now.Add(minimumFailingDuration).Add(time.Second)
	b.lock.Lock()
	b.clock.Set(newTime)
	b.lock.Unlock()

	// Register another failure for all three
	for _, vdrID := range []ids.NodeID{vdrID0, vdrID1, vdrID2} {
		b.RegisterFailure(vdrID)
	}

	// Only vdr0 and vdr1 should be benched (total weight 2000)
	// Benching vdr2 (weight 1000) would cause the amount benched
	// to exceed the maximum
	b.lock.Lock()
	require.True(b.isBenched(vdrID0))
	require.True(b.isBenched(vdrID1))
	require.False(b.isBenched(vdrID2))
	require.Equal(b.benchedQueue.Len(), 2)
	require.Equal(b.benchlistSet.Len(), 2)
	require.Len(b.failureStreaks, 1)
	fs := b.failureStreaks[vdrID2]
	fs.consecutive = threshold
	fs.firstFailure = now
	b.lock.Unlock()

	// Register threshold - 1 failures for vdr4
	for i := 0; i < threshold-1; i++ {
		b.RegisterFailure(vdrID4)
	}

	// Advance the time past min failing duration
	newTime2 := newTime.Add(minimumFailingDuration).Add(time.Second)
	b.lock.Lock()
	b.clock.Set(newTime2)
	b.lock.Unlock()

	// Register another failure for vdr4
	b.RegisterFailure(vdrID4)

	// vdr4 should be benched now
	b.lock.Lock()
	require.True(b.isBenched(vdrID0))
	require.True(b.isBenched(vdrID1))
	require.True(b.isBenched(vdrID4))
	require.Equal(3, b.benchedQueue.Len())
	require.Equal(3, b.benchlistSet.Len())
	require.Contains(b.benchlistSet, vdrID0)
	require.Contains(b.benchlistSet, vdrID1)
	require.Contains(b.benchlistSet, vdrID4)
	require.Len(b.failureStreaks, 1) // for vdr2
	b.lock.Unlock()

	// More failures for vdr2 shouldn't add it to the bench
	// because the max bench amount would be exceeded
	for i := 0; i < threshold-1; i++ {
		b.RegisterFailure(vdrID2)
	}

	b.lock.Lock()
	require.True(b.isBenched(vdrID0))
	require.True(b.isBenched(vdrID1))
	require.True(b.isBenched(vdrID4))
	require.False(b.isBenched(vdrID2))
	require.Equal(3, b.benchedQueue.Len())
	require.Equal(3, b.benchlistSet.Len())
	require.Len(b.failureStreaks, 1)
	require.Contains(b.failureStreaks, vdrID2)

	// Ensure the benched queue root has the min end time
	minEndTime := b.benchedQueue[0].benchedUntil
	benchedIDs := []ids.NodeID{vdrID0, vdrID1, vdrID4}
	for _, benchedVdr := range b.benchedQueue {
		require.Contains(benchedIDs, benchedVdr.nodeID)
		require.False(benchedVdr.benchedUntil.Before(minEndTime))
	}

	b.lock.Unlock()
}

// Test validators are removed from the bench correctly
func TestBenchlistRemove(t *testing.T) {
	require := require.New(t)

	vdrs := validators.NewSet()
	vdrID0 := ids.GenerateTestNodeID()
	vdrID1 := ids.GenerateTestNodeID()
	vdrID2 := ids.GenerateTestNodeID()
	vdrID3 := ids.GenerateTestNodeID()
	vdrID4 := ids.GenerateTestNodeID()

	// Total weight is 5000
	require.NoError(vdrs.Add(vdrID0, nil, ids.Empty, 1000))
	require.NoError(vdrs.Add(vdrID1, nil, ids.Empty, 1000))
	require.NoError(vdrs.Add(vdrID2, nil, ids.Empty, 1000))
	require.NoError(vdrs.Add(vdrID3, nil, ids.Empty, 1000))
	require.NoError(vdrs.Add(vdrID4, nil, ids.Empty, 1000))

	count := 0
	benchable := &TestBenchable{
		T:             t,
		CantUnbenched: true,
		UnbenchedF: func(ids.ID, ids.NodeID) {
			count++
		},
	}

	threshold := 3
	duration := 2 * time.Second
	maxPortion := 0.76 // can bench 3 of the 5 validators
	benchIntf, err := NewBenchlist(
		ids.Empty,
		logging.NoLog{},
		benchable,
		vdrs,
		threshold,
		minimumFailingDuration,
		duration,
		maxPortion,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	b := benchIntf.(*benchlist)
	defer b.timer.Stop()
	now := time.Now()
	b.lock.Lock()
	b.clock.Set(now)
	b.lock.Unlock()

	// Register [threshold-1] failures for 3 validators
	for _, vdrID := range []ids.NodeID{vdrID0, vdrID1, vdrID2} {
		for i := 0; i < threshold-1; i++ {
			b.RegisterFailure(vdrID)
		}
	}

	// Advance the time past the min failing duration and register another failure
	// for each
	now = now.Add(minimumFailingDuration).Add(time.Second)
	b.lock.Lock()
	b.clock.Set(now)
	b.lock.Unlock()
	for _, vdrID := range []ids.NodeID{vdrID0, vdrID1, vdrID2} {
		b.RegisterFailure(vdrID)
	}

	// All 3 should be benched
	b.lock.Lock()
	require.True(b.isBenched(vdrID0))
	require.True(b.isBenched(vdrID1))
	require.True(b.isBenched(vdrID2))
	require.Equal(3, b.benchedQueue.Len())
	require.Equal(3, b.benchlistSet.Len())
	require.Empty(b.failureStreaks)

	// Ensure the benched queue root has the min end time
	minEndTime := b.benchedQueue[0].benchedUntil
	benchedIDs := []ids.NodeID{vdrID0, vdrID1, vdrID2}
	for _, benchedVdr := range b.benchedQueue {
		require.Contains(benchedIDs, benchedVdr.nodeID)
		require.False(benchedVdr.benchedUntil.Before(minEndTime))
	}

	// Set the benchlist's clock past when all validators should be unbenched
	// so that when its timer fires, it can remove them
	b.clock.Set(b.clock.Time().Add(duration))
	b.lock.Unlock()

	// Make sure each validator is eventually removed
	require.Eventually(
		func() bool {
			return !b.IsBenched(vdrID0)
		},
		duration+time.Second, // extra time.Second as grace period
		100*time.Millisecond,
	)

	require.Eventually(
		func() bool {
			return !b.IsBenched(vdrID1)
		},
		duration+time.Second,
		100*time.Millisecond,
	)

	require.Eventually(
		func() bool {
			return !b.IsBenched(vdrID2)
		},
		duration+time.Second,
		100*time.Millisecond,
	)

	require.Equal(3, count)
}
