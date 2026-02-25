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
	const halflife = 25 * time.Millisecond

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
			Halflife:           halflife,
			UnbenchProbability: DefaultUnbenchProbability,
			BenchProbability:   DefaultBenchProbability,
			BenchDuration:      DefaultBenchDuration,
			MaxPortion:         0.999,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	defer b.shutdown()
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

	// First failure on a new node starts from an optimistic prior, so p = 0.5
	// and no benching occurs.
	b.RegisterFailure(vdrID)
	requireNoBenchings()

	b.RegisterFailure(vdrID) // p ≈ 0.67 > 0.5 → bench
	<-benchable.updated
	requireBenched()

	b.RegisterFailure(nodeID) // Non-validators shouldn't be tracked
	requireBenched()

	for range 8 {
		b.RegisterResponse(vdrID)
	} // p ≈ 0.19 < 0.2 → unbench
	<-benchable.updated
	requireNoBenchings()

	// Let prior observations decay by one halflife before recording failures.
	time.Sleep(halflife)

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
	defer b.shutdown()
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
	defer b.shutdown()

	// Bench the node: p > 0.5
	b.RegisterFailure(vdrID)
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
// so that it gets a clean slate.
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
	defer b.shutdown()

	// Bench the node: p > 0.5
	b.RegisterFailure(vdrID)
	b.RegisterFailure(vdrID)
	<-benchable.updated
	require.True(b.IsBenched(vdrID))

	// Wait for timeout-based unbench.
	<-benchable.updated
	require.False(b.IsBenched(vdrID))

	// After timeout, the EWMA is reset. A response then a failure still keeps
	// p < 0.5, which is not enough to re-bench.
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
	defer b.shutdown()

	// Bench the node and wait for the consumer to block in Benched.
	b.RegisterFailure(vdrID)
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
