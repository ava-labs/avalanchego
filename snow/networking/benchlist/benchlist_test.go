// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var minimumFailingDuration = 5 * time.Minute

// Test that validators are properly added to the bench
func TestBenchlistAdd(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID0 := ids.GenerateTestNodeID()
	vdrID1 := ids.GenerateTestNodeID()
	vdrID2 := ids.GenerateTestNodeID()
	vdrID3 := ids.GenerateTestNodeID()
	vdrID4 := ids.GenerateTestNodeID()

	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID0, nil, ids.Empty, 50))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID1, nil, ids.Empty, 50))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID2, nil, ids.Empty, 50))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID3, nil, ids.Empty, 50))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID4, nil, ids.Empty, 50))

	benchable := &TestBenchable{T: t}
	benchable.Default(true)

	threshold := 3
	duration := time.Minute
	maxPortion := 0.5
	benchIntf, err := NewBenchlist(
		ctx,
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
	now := time.Now()
	b.clock.Set(now)

	// Nobody should be benched at the start
	b.lock.Lock()
	require.Empty(b.benchlistSet)
	require.Empty(b.failureStreaks)
	require.Zero(b.benchedHeap.Len())
	b.lock.Unlock()

	// Register [threshold - 1] failures in a row for vdr0
	for i := 0; i < threshold-1; i++ {
		b.RegisterFailure(vdrID0)
	}

	// Still shouldn't be benched due to not enough consecutive failure
	require.Empty(b.benchlistSet)
	require.Zero(b.benchedHeap.Len())
	require.Len(b.failureStreaks, 1)
	fs := b.failureStreaks[vdrID0]
	require.Equal(threshold-1, fs.consecutive)
	require.True(fs.firstFailure.Equal(now))

	// Register another failure
	b.RegisterFailure(vdrID0)

	// Still shouldn't be benched because not enough time (any in this case)
	// has passed since the first failure
	b.lock.Lock()
	require.Empty(b.benchlistSet)
	require.Zero(b.benchedHeap.Len())
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
	require.Contains(b.benchlistSet, vdrID0)
	require.Equal(1, b.benchedHeap.Len())
	require.Equal(1, b.benchlistSet.Len())

	nodeID, benchedUntil, ok := b.benchedHeap.Peek()
	require.True(ok)
	require.Equal(vdrID0, nodeID)
	require.False(benchedUntil.After(now.Add(duration)))
	require.False(benchedUntil.Before(now.Add(duration / 2)))
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
	require.Contains(b.benchlistSet, vdrID0)
	require.Equal(1, b.benchedHeap.Len())
	require.Equal(1, b.benchlistSet.Len())
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

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID0 := ids.GenerateTestNodeID()
	vdrID1 := ids.GenerateTestNodeID()
	vdrID2 := ids.GenerateTestNodeID()
	vdrID3 := ids.GenerateTestNodeID()
	vdrID4 := ids.GenerateTestNodeID()

	// Total weight is 5100
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID0, nil, ids.Empty, 1000))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID1, nil, ids.Empty, 1000))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID2, nil, ids.Empty, 1000))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID3, nil, ids.Empty, 2000))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID4, nil, ids.Empty, 100))

	threshold := 3
	duration := 1 * time.Hour
	// Shouldn't bench more than 2550 (5100/2)
	maxPortion := 0.5
	benchIntf, err := NewBenchlist(
		ctx,
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
	require.Contains(b.benchlistSet, vdrID0)
	require.Contains(b.benchlistSet, vdrID1)
	require.Equal(2, b.benchedHeap.Len())
	require.Equal(2, b.benchlistSet.Len())
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
	require.Contains(b.benchlistSet, vdrID0)
	require.Contains(b.benchlistSet, vdrID1)
	require.Contains(b.benchlistSet, vdrID4)
	require.Equal(3, b.benchedHeap.Len())
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
	require.Contains(b.benchlistSet, vdrID0)
	require.Contains(b.benchlistSet, vdrID1)
	require.Contains(b.benchlistSet, vdrID4)
	require.Equal(3, b.benchedHeap.Len())
	require.Equal(3, b.benchlistSet.Len())
	require.Len(b.failureStreaks, 1)
	require.Contains(b.failureStreaks, vdrID2)
	b.lock.Unlock()
}

// Test validators are removed from the bench correctly
func TestBenchlistRemove(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID0 := ids.GenerateTestNodeID()
	vdrID1 := ids.GenerateTestNodeID()
	vdrID2 := ids.GenerateTestNodeID()
	vdrID3 := ids.GenerateTestNodeID()
	vdrID4 := ids.GenerateTestNodeID()

	// Total weight is 5000
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID0, nil, ids.Empty, 1000))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID1, nil, ids.Empty, 1000))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID2, nil, ids.Empty, 1000))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID3, nil, ids.Empty, 1000))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID4, nil, ids.Empty, 1000))

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
		ctx,
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
	require.Contains(b.benchlistSet, vdrID0)
	require.Contains(b.benchlistSet, vdrID1)
	require.Contains(b.benchlistSet, vdrID2)
	require.Equal(3, b.benchedHeap.Len())
	require.Equal(3, b.benchlistSet.Len())
	require.Empty(b.failureStreaks)

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
