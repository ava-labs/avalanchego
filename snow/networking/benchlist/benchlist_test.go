// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
)

type benchable struct {
	t *testing.T

	wantChainID ids.ID
	benched     set.Set[ids.NodeID]

	// updated allows for synchronization when a bench/unbench occurs
	updated chan struct{}
}

func (b *benchable) Benched(chainID ids.ID, nodeID ids.NodeID) {
	require.Equal(b.t, b.wantChainID, chainID)
	require.NotContains(b.t, b.benched, nodeID)
	b.benched.Add(nodeID)
	b.updated <- struct{}{}
}

func (b *benchable) Unbenched(chainID ids.ID, nodeID ids.NodeID) {
	require.Equal(b.t, b.wantChainID, chainID)
	require.Contains(b.t, b.benched, nodeID)
	b.benched.Remove(nodeID)
	b.updated <- struct{}{}
}

type blockingBenchable struct {
	t *testing.T

	wantChainID ids.ID

	benchedCalled chan struct{}
	unblock       chan struct{}

	once sync.Once
}

func (b *blockingBenchable) Benched(chainID ids.ID, _ ids.NodeID) {
	require.Equal(b.t, b.wantChainID, chainID)
	b.once.Do(func() {
		close(b.benchedCalled)
	})
	<-b.unblock
}

func (*blockingBenchable) Unbenched(ids.ID, ids.NodeID) {}

func TestBenchlist(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID := ids.GenerateTestNodeID()
	nodeID := ids.GenerateTestNodeID()

	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	benchable := &benchable{
		t:           t,
		wantChainID: ctx.ChainID,
		updated:     make(chan struct{}, 1),
	}
	b, err := newBenchlist(
		ctx,
		benchable,
		vdrs,
		Config{
			Halflife:           DefaultHalflife,
			UnbenchProbability: DefaultUnbenchProbability,
			BenchProbability:   DefaultBenchProbability,
			BenchDuration:      DefaultBenchDuration,
			MaxPortion:         0.999,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	now := time.Now()
	b.clock.Set(now)

	requireNoBenchings := func() {
		t.Helper()
		require.False(b.IsBenched(vdrID))
		require.False(b.IsBenched(nodeID))
	}

	requireBenched := func() {
		t.Helper()
		require.True(b.IsBenched(vdrID))
		require.False(b.IsBenched(nodeID))
	}

	// Nobody should be benched at the start
	requireNoBenchings()

	// Observations before the matured averager's halflife elapses produce
	// Read() = 0, so no benching occurs regardless of failure rate.
	b.RegisterResponse(vdrID)
	b.RegisterFailure(vdrID)
	b.RegisterFailure(vdrID)
	requireNoBenchings()

	// Advance past halflife so the averager matures. A failure now produces a
	// non-zero probability that exceeds the bench threshold.
	now = now.Add(DefaultHalflife)
	b.clock.Set(now)

	b.RegisterFailure(vdrID) // matured, p ≈ 0.8 > 0.5 → bench
	<-benchable.updated
	requireBenched()

	b.RegisterFailure(nodeID) // Non-validators shouldn't be tracked
	requireBenched()

	for range 8 {
		b.RegisterResponse(vdrID)
	} // p ≈ 0.19 < 0.2 → unbench
	<-benchable.updated
	requireNoBenchings()

	// After another halflife of decay, failures can re-bench.
	now = now.Add(DefaultHalflife)
	b.clock.Set(now)

	for range 4 {
		b.RegisterFailure(vdrID)
	} // p ≈ 0.54 > 0.5 → bench
	<-benchable.updated
	requireBenched()
}

func TestBenchlistSkipsBenchingWhenMaxPortionExceeded(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID0 := ids.GenerateTestNodeID()
	vdrID1 := ids.GenerateTestNodeID()

	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID0, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID1, nil, ids.Empty, 1))

	benchable := &benchable{
		t:           t,
		wantChainID: ctx.ChainID,
		updated:     make(chan struct{}, 1),
	}
	b, err := newBenchlist(
		ctx,
		benchable,
		vdrs,
		Config{
			Halflife:           DefaultHalflife,
			UnbenchProbability: DefaultUnbenchProbability,
			BenchProbability:   DefaultBenchProbability,
			BenchDuration:      DefaultBenchDuration,
			MaxPortion:         0.4,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	now := time.Now()
	b.clock.Set(now)

	// First observation starts the maturation timer.
	b.RegisterResponse(vdrID0)

	// Advance past halflife so the averager matures.
	now = now.Add(DefaultHalflife)
	b.clock.Set(now)

	// p > 0.5, but benching this validator would bench 50% of stake and
	// exceed maxPortion (40%).
	b.RegisterFailure(vdrID0)
	b.RegisterFailure(vdrID0)

	select {
	case <-benchable.updated:
		require.FailNow("unexpected bench/unbench callback")
	case <-time.After(50 * time.Millisecond):
	}

	require.False(b.IsBenched(vdrID0))
	require.Zero(testutil.ToFloat64(b.numBenched))
	require.Zero(testutil.ToFloat64(b.weightBenched))
}

func TestBenchlistTimeout(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID := ids.GenerateTestNodeID()

	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	benchable := &benchable{
		t:           t,
		wantChainID: ctx.ChainID,
		updated:     make(chan struct{}, 1),
	}
	b, err := newBenchlist(
		ctx,
		benchable,
		vdrs,
		Config{
			Halflife:           DefaultHalflife,
			UnbenchProbability: DefaultUnbenchProbability,
			BenchProbability:   DefaultBenchProbability,
			BenchDuration:      50 * time.Millisecond,
			MaxPortion:         0.999,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	now := time.Now()
	b.clock.Set(now)

	// First observation starts the maturation timer.
	b.RegisterResponse(vdrID)

	// Advance past halflife so the averager matures.
	now = now.Add(DefaultHalflife)
	b.clock.Set(now)

	// Bench the node: matured, p > 0.5
	b.RegisterFailure(vdrID)
	<-benchable.updated
	require.True(b.IsBenched(vdrID))

	// The consumer goroutine's timer fires (real time ≥ 50ms) and calls
	// Unbenched on the benchable.
	<-benchable.updated
	require.False(b.IsBenched(vdrID))
	require.Empty(benchable.benched)
}

// Test that when a node is unbenched via timeout, its EWMA history is wiped
// so that it gets a clean slate. The matured averager wrapper additionally
// ensures the node must observe traffic for at least [halflife] before it can
// be benched again.
func TestBenchlistTimeoutCleansSlate(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID := ids.GenerateTestNodeID()

	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	benchable := &benchable{
		t:           t,
		wantChainID: ctx.ChainID,
		updated:     make(chan struct{}, 1),
	}
	b, err := newBenchlist(
		ctx,
		benchable,
		vdrs,
		Config{
			Halflife:           DefaultHalflife,
			UnbenchProbability: DefaultUnbenchProbability,
			BenchProbability:   DefaultBenchProbability,
			BenchDuration:      50 * time.Millisecond,
			MaxPortion:         0.999,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	now := time.Now()
	b.clock.Set(now)

	// First observation starts the maturation timer.
	b.RegisterResponse(vdrID)

	// Advance past halflife so the averager matures.
	now = now.Add(DefaultHalflife)
	b.clock.Set(now)

	// Bench the node: matured, p > 0.5
	b.RegisterFailure(vdrID)
	<-benchable.updated
	require.True(b.IsBenched(vdrID))

	// Wait for timeout-based unbench.
	<-benchable.updated
	require.False(b.IsBenched(vdrID))

	// After timeout, the EWMA is reset with a fresh matured averager. Even if
	// every subsequent observation is a failure, the node cannot be re-benched
	// until the new averager matures (i.e. halflife elapses).
	b.RegisterResponse(vdrID)
	b.RegisterFailure(vdrID)
	require.False(b.IsBenched(vdrID))
}

func TestObserveDoesNotBlockWhenConsumerIsBlocked(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	benchable := &blockingBenchable{
		t:             t,
		wantChainID:   ctx.ChainID,
		benchedCalled: make(chan struct{}),
		unblock:       make(chan struct{}),
	}
	b, err := newBenchlist(
		ctx,
		benchable,
		vdrs,
		Config{
			Halflife:           DefaultHalflife,
			UnbenchProbability: DefaultUnbenchProbability,
			BenchProbability:   DefaultBenchProbability,
			BenchDuration:      DefaultBenchDuration,
			MaxPortion:         0.999,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	now := time.Now()
	b.clock.Set(now)

	// First observation starts the maturation timer.
	b.RegisterResponse(vdrID)

	// Advance past halflife so the averager matures.
	now = now.Add(DefaultHalflife)
	b.clock.Set(now)

	// Bench the node and wait for the consumer to block in Benched.
	b.RegisterFailure(vdrID)
	select {
	case <-benchable.benchedCalled:
	case <-time.After(time.Second):
		require.FailNow("timed out waiting for consumer to block in Benched")
	}

	done := make(chan struct{})
	go func() {
		// Enqueue many events while the consumer is blocked. These all go
		// into the unbounded queue without blocking.
		for range 8 {
			b.RegisterResponse(vdrID)
		}
		for range 8 {
			b.RegisterFailure(vdrID)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		require.FailNow("observe path blocked while consumer was blocked")
	}

	close(benchable.unblock)
}

// TestRunDrainsEntireEventQueuePerSignal verifies that when the consumer
// goroutine wakes up on a single eventReady signal, it drains all pending
// events in one pass — producing both the bench and unbench notifications.
func TestRunDrainsEntireEventQueuePerSignal(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	benchable := &benchable{
		t:           t,
		wantChainID: ctx.ChainID,
		updated:     make(chan struct{}, 2),
	}
	b, err := newBenchlist(
		ctx,
		benchable,
		vdrs,
		Config{
			Halflife:           DefaultHalflife,
			UnbenchProbability: DefaultUnbenchProbability,
			BenchProbability:   DefaultBenchProbability,
			BenchDuration:      DefaultBenchDuration,
			MaxPortion:         0.999,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	now := time.Now()
	b.clock.Set(now)

	// Register first observation via the normal API to initialize the node
	// entry in the consumer goroutine.
	b.RegisterResponse(vdrID)
	require.Eventually(func() bool { return b.events.Len() == 0 }, time.Second, time.Millisecond)

	// Advance past halflife so the averager matures.
	now = now.Add(DefaultHalflife)
	b.clock.Set(now)

	// Push events directly to the queue without signaling.
	// 2 failures → p > 0.5 → bench, then 8 successes → p < 0.2 → unbench.
	require.True(b.events.PushRight(event{nodeID: vdrID, value: failure, time: now}))
	require.True(b.events.PushRight(event{nodeID: vdrID, value: failure, time: now}))
	for range 8 {
		require.True(b.events.PushRight(event{nodeID: vdrID, value: success, time: now}))
	}

	// Send a single signal — the consumer should drain all 10 events.
	b.eventReady <- struct{}{}

	select {
	case <-benchable.updated:
	case <-time.After(time.Second):
		require.FailNow("timed out waiting for bench notification")
	}
	select {
	case <-benchable.updated:
	case <-time.After(time.Second):
		require.FailNow("timed out waiting for unbench notification")
	}
}
