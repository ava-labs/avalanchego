// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var minimumFailingDuration = 5 * time.Minute

// Test that validators are properly added to the bench
func TestBenchlistAdd(t *testing.T) {
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(50)
	vdr1 := validators.GenerateRandomValidator(50)
	vdr2 := validators.GenerateRandomValidator(50)
	vdr3 := validators.GenerateRandomValidator(50)
	vdr4 := validators.GenerateRandomValidator(50)

	errs := wrappers.Errs{}
	errs.Add(
		vdrs.AddWeight(vdr0.ID(), vdr0.Weight()),
		vdrs.AddWeight(vdr1.ID(), vdr1.Weight()),
		vdrs.AddWeight(vdr2.ID(), vdr2.Weight()),
		vdrs.AddWeight(vdr3.ID(), vdr3.Weight()),
		vdrs.AddWeight(vdr4.ID(), vdr4.Weight()),
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

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
	if err != nil {
		t.Fatal(err)
	}
	b := benchIntf.(*benchlist)
	defer b.timer.Stop()
	now := time.Now()
	b.clock.Set(now)

	// Nobody should be benched at the start
	b.lock.Lock()
	require.False(t, b.isBenched(vdr0.ID()))
	require.False(t, b.isBenched(vdr1.ID()))
	require.False(t, b.isBenched(vdr2.ID()))
	require.False(t, b.isBenched(vdr3.ID()))
	require.False(t, b.isBenched(vdr4.ID()))
	require.Len(t, b.failureStreaks, 0)
	require.Equal(t, b.benchedQueue.Len(), 0)
	require.Equal(t, b.benchlistSet.Len(), 0)
	b.lock.Unlock()

	// Register [threshold - 1] failures in a row for vdr0
	for i := 0; i < threshold-1; i++ {
		b.RegisterFailure(vdr0.ID())
	}

	// Still shouldn't be benched due to not enough consecutive failure
	require.False(t, b.isBenched(vdr0.ID()))
	require.Equal(t, b.benchedQueue.Len(), 0)
	require.Equal(t, b.benchlistSet.Len(), 0)
	require.Len(t, b.failureStreaks, 1)
	fs := b.failureStreaks[vdr0.ID()]
	require.Equal(t, threshold-1, fs.consecutive)
	require.True(t, fs.firstFailure.Equal(now))

	// Register another failure
	b.RegisterFailure(vdr0.ID())

	// Still shouldn't be benched because not enough time (any in this case)
	// has passed since the first failure
	b.lock.Lock()
	require.False(t, b.isBenched(vdr0.ID()))
	require.Equal(t, b.benchedQueue.Len(), 0)
	require.Equal(t, b.benchlistSet.Len(), 0)
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
	b.RegisterFailure(vdr0.ID())

	// Now this validator should be benched
	b.lock.Lock()
	require.True(t, b.isBenched(vdr0.ID()))
	require.Equal(t, b.benchedQueue.Len(), 1)
	require.Equal(t, b.benchlistSet.Len(), 1)

	next := b.benchedQueue[0]
	require.Equal(t, vdr0.ID(), next.nodeID)
	require.True(t, !next.benchedUntil.After(now.Add(duration)))
	require.True(t, !next.benchedUntil.Before(now.Add(duration/2)))
	require.Len(t, b.failureStreaks, 0)
	require.True(t, benched)
	benchable.BenchedF = nil
	b.lock.Unlock()

	// Give another validator [threshold-1] failures
	for i := 0; i < threshold-1; i++ {
		b.RegisterFailure(vdr1.ID())
	}

	// Register another failure
	b.RegisterResponse(vdr1.ID())

	// vdr1 shouldn't be benched
	// The response should have cleared its consecutive failures
	b.lock.Lock()
	require.True(t, b.isBenched(vdr0.ID()))
	require.False(t, b.isBenched(vdr1.ID()))
	require.Equal(t, b.benchedQueue.Len(), 1)
	require.Equal(t, b.benchlistSet.Len(), 1)
	require.Len(t, b.failureStreaks, 0)
	b.lock.Unlock()

	// Register another failure for vdr0, who is benched
	b.RegisterFailure(vdr0.ID())

	// A failure for an already benched validator should not count against it
	b.lock.Lock()
	require.Len(t, b.failureStreaks, 0)
	b.lock.Unlock()
}

// Test that the benchlist won't bench more than the maximum portion of stake
func TestBenchlistMaxStake(t *testing.T) {
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(1000)
	vdr1 := validators.GenerateRandomValidator(1000)
	vdr2 := validators.GenerateRandomValidator(1000)
	vdr3 := validators.GenerateRandomValidator(2000)
	vdr4 := validators.GenerateRandomValidator(100)
	// Total weight is 5100

	errs := wrappers.Errs{}
	errs.Add(
		vdrs.AddWeight(vdr0.ID(), vdr0.Weight()),
		vdrs.AddWeight(vdr1.ID(), vdr1.Weight()),
		vdrs.AddWeight(vdr2.ID(), vdr2.Weight()),
		vdrs.AddWeight(vdr3.ID(), vdr3.Weight()),
		vdrs.AddWeight(vdr4.ID(), vdr4.Weight()),
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

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
	if err != nil {
		t.Fatal(err)
	}
	b := benchIntf.(*benchlist)
	defer b.timer.Stop()
	now := time.Now()
	b.clock.Set(now)

	// Register [threshold-1] failures for 3 validators
	for _, vdr := range []validators.Validator{vdr0, vdr1, vdr2} {
		for i := 0; i < threshold-1; i++ {
			b.RegisterFailure(vdr.ID())
		}
	}

	// Advance the time to past the minimum failing duration
	newTime := now.Add(minimumFailingDuration).Add(time.Second)
	b.lock.Lock()
	b.clock.Set(newTime)
	b.lock.Unlock()

	// Register another failure for all three
	for _, vdr := range []validators.Validator{vdr0, vdr1, vdr2} {
		b.RegisterFailure(vdr.ID())
	}

	// Only vdr0 and vdr1 should be benched (total weight 2000)
	// Benching vdr2 (weight 1000) would cause the amount benched
	// to exceed the maximum
	b.lock.Lock()
	require.True(t, b.isBenched(vdr0.ID()))
	require.True(t, b.isBenched(vdr1.ID()))
	require.False(t, b.isBenched(vdr2.ID()))
	require.Equal(t, b.benchedQueue.Len(), 2)
	require.Equal(t, b.benchlistSet.Len(), 2)
	require.Len(t, b.failureStreaks, 1)
	fs := b.failureStreaks[vdr2.ID()]
	fs.consecutive = threshold
	fs.firstFailure = now
	b.lock.Unlock()

	// Register threshold - 1 failures for vdr4
	for i := 0; i < threshold-1; i++ {
		b.RegisterFailure(vdr4.ID())
	}

	// Advance the time past min failing duration
	newTime2 := newTime.Add(minimumFailingDuration).Add(time.Second)
	b.lock.Lock()
	b.clock.Set(newTime2)
	b.lock.Unlock()

	// Register another failure for vdr4
	b.RegisterFailure(vdr4.ID())

	// vdr4 should be benched now
	b.lock.Lock()
	require.True(t, b.isBenched(vdr0.ID()))
	require.True(t, b.isBenched(vdr1.ID()))
	require.True(t, b.isBenched(vdr4.ID()))
	require.Equal(t, 3, b.benchedQueue.Len())
	require.Equal(t, 3, b.benchlistSet.Len())
	require.Contains(t, b.benchlistSet, vdr0.ID())
	require.Contains(t, b.benchlistSet, vdr1.ID())
	require.Contains(t, b.benchlistSet, vdr4.ID())
	require.Len(t, b.failureStreaks, 1) // for vdr2
	b.lock.Unlock()

	// More failures for vdr2 shouldn't add it to the bench
	// because the max bench amount would be exceeded
	for i := 0; i < threshold-1; i++ {
		b.RegisterFailure(vdr2.ID())
	}

	b.lock.Lock()
	require.True(t, b.isBenched(vdr0.ID()))
	require.True(t, b.isBenched(vdr1.ID()))
	require.True(t, b.isBenched(vdr4.ID()))
	require.False(t, b.isBenched(vdr2.ID()))
	require.Equal(t, 3, b.benchedQueue.Len())
	require.Equal(t, 3, b.benchlistSet.Len())
	require.Len(t, b.failureStreaks, 1)
	require.Contains(t, b.failureStreaks, vdr2.ID())

	// Ensure the benched queue root has the min end time
	minEndTime := b.benchedQueue[0].benchedUntil
	benchedIDs := []ids.NodeID{vdr0.ID(), vdr1.ID(), vdr4.ID()}
	for _, benchedVdr := range b.benchedQueue {
		require.Contains(t, benchedIDs, benchedVdr.nodeID)
		require.True(t, !benchedVdr.benchedUntil.Before(minEndTime))
	}

	b.lock.Unlock()
}

// Test validators are removed from the bench correctly
func TestBenchlistRemove(t *testing.T) {
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(1000)
	vdr1 := validators.GenerateRandomValidator(1000)
	vdr2 := validators.GenerateRandomValidator(1000)
	vdr3 := validators.GenerateRandomValidator(1000)
	vdr4 := validators.GenerateRandomValidator(1000)
	// Total weight is 5100

	errs := wrappers.Errs{}
	errs.Add(
		vdrs.AddWeight(vdr0.ID(), vdr0.Weight()),
		vdrs.AddWeight(vdr1.ID(), vdr1.Weight()),
		vdrs.AddWeight(vdr2.ID(), vdr2.Weight()),
		vdrs.AddWeight(vdr3.ID(), vdr3.Weight()),
		vdrs.AddWeight(vdr4.ID(), vdr4.Weight()),
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

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
	if err != nil {
		t.Fatal(err)
	}
	b := benchIntf.(*benchlist)
	defer b.timer.Stop()
	now := time.Now()
	b.lock.Lock()
	b.clock.Set(now)
	b.lock.Unlock()

	// Register [threshold-1] failures for 3 validators
	for _, vdr := range []validators.Validator{vdr0, vdr1, vdr2} {
		for i := 0; i < threshold-1; i++ {
			b.RegisterFailure(vdr.ID())
		}
	}

	// Advance the time past the min failing duration and register another failure
	// for each
	now = now.Add(minimumFailingDuration).Add(time.Second)
	b.lock.Lock()
	b.clock.Set(now)
	b.lock.Unlock()
	for _, vdr := range []validators.Validator{vdr0, vdr1, vdr2} {
		b.RegisterFailure(vdr.ID())
	}

	// All 3 should be benched
	b.lock.Lock()
	require.True(t, b.isBenched(vdr0.ID()))
	require.True(t, b.isBenched(vdr1.ID()))
	require.True(t, b.isBenched(vdr2.ID()))
	require.Equal(t, 3, b.benchedQueue.Len())
	require.Equal(t, 3, b.benchlistSet.Len())
	require.Len(t, b.failureStreaks, 0)

	// Ensure the benched queue root has the min end time
	minEndTime := b.benchedQueue[0].benchedUntil
	benchedIDs := []ids.NodeID{vdr0.ID(), vdr1.ID(), vdr2.ID()}
	for _, benchedVdr := range b.benchedQueue {
		require.Contains(t, benchedIDs, benchedVdr.nodeID)
		require.True(t, !benchedVdr.benchedUntil.Before(minEndTime))
	}

	// Set the benchlist's clock past when all validators should be unbenched
	// so that when its timer fires, it can remove them
	b.clock.Set(b.clock.Time().Add(duration))
	b.lock.Unlock()

	// Make sure each validator is eventually removed
	require.Eventually(
		t,
		func() bool {
			return !b.IsBenched(vdr0.ID())
		},
		duration+time.Second, // extra time.Second as grace period
		100*time.Millisecond,
	)

	require.Eventually(
		t,
		func() bool {
			return !b.IsBenched(vdr1.ID())
		},
		duration+time.Second,
		100*time.Millisecond,
	)

	require.Eventually(
		t,
		func() bool {
			return !b.IsBenched(vdr2.ID())
		},
		duration+time.Second,
		100*time.Millisecond,
	)

	require.Equal(t, 3, count)
}
