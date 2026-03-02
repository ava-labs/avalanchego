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

const defaultTestTimeout = 10 * time.Second

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

// TestBenchlistEvictsLowestProbabilityNode verifies that when a new node
// crosses the bench threshold but capacity is full, the benched node with
// the lowest failure probability is evicted to make room.
func TestBenchlistEvictsLowestProbabilityNode(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()

	// 3 validators with stake=1 each. Total stake = 3.
	// maxPortion = 0.4 → maxBenchedStake = 1.2, so only 1 node (stake=1) fits.
	vdrA := ids.GenerateTestNodeID()
	vdrB := ids.GenerateTestNodeID()
	vdrC := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrA, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrB, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrC, nil, ids.Empty, 1))

	benchable := &benchable{
		t:           t,
		wantChainID: ctx.ChainID,
		// Capacity 2: eviction produces Unbenched(victim) + Benched(incoming).
		updated: make(chan struct{}, 2),
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

	// Bench A: 2 failures → p ≈ 0.67 > 0.5 → benched.
	b.RegisterFailure(vdrA)
	b.RegisterFailure(vdrA)
	<-benchable.updated
	require.True(b.IsBenched(vdrA))

	// Now push B to a higher failure probability than A.
	// 3 failures → p ≈ 0.75 > A's 0.67. B wants to bench but capacity is
	// full. Eviction: A (lower p) is evicted, B is benched.
	b.RegisterFailure(vdrB)
	b.RegisterFailure(vdrB)
	b.RegisterFailure(vdrB)
	// Expect Unbenched(A) + Benched(B) = 2 signals.
	<-benchable.updated
	<-benchable.updated

	require.False(b.IsBenched(vdrA), "A should be evicted")
	require.True(b.IsBenched(vdrB), "B should be benched")
}

// TestBenchlistEvictsMultipleNodes verifies that when a high-stake node
// crosses the bench threshold, multiple lower-probability benched nodes
// are evicted to make room within the maxPortion constraint.
func TestBenchlistEvictsMultipleNodes(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()

	// 4 validators: A(2), B(2), C(1), D(5). Total = 10.
	// maxPortion = 0.5 → maxBenchedStake = 5.
	// A+B benched = 4 stake, fits within 5. D (stake=5) requires evicting
	// both A and B: evicting only one frees 2 stake, leaving 2+5=7 > 5.
	// Evicting both frees 4, leaving 0+5=5 ≤ 5.
	vdrA := ids.GenerateTestNodeID()
	vdrB := ids.GenerateTestNodeID()
	vdrC := ids.GenerateTestNodeID()
	vdrD := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrA, nil, ids.Empty, 2))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrB, nil, ids.Empty, 2))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrC, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrD, nil, ids.Empty, 5))

	benchable := &benchable{
		t:           t,
		wantChainID: ctx.ChainID,
		// Capacity 4: eviction of 2 nodes + bench of D + buffer.
		updated: make(chan struct{}, 4),
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
			MaxPortion:         0.5,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	defer b.shutdown()

	// Bench A: 2 failures → p ≈ 0.67 > 0.5 → benched.
	b.RegisterFailure(vdrA)
	b.RegisterFailure(vdrA)
	<-benchable.updated
	require.True(b.IsBenched(vdrA))

	// Bench B: 2 failures → p ≈ 0.67 > 0.5 → benched.
	// Total benched stake = 4, within maxBenchedStake = 5.
	b.RegisterFailure(vdrB)
	b.RegisterFailure(vdrB)
	<-benchable.updated
	require.True(b.IsBenched(vdrB))

	// D: 5 failures → p ≈ 0.83, well above A and B's ~0.67.
	// D (stake=5) needs room: benchedStake = 4+5 = 9 > 5.
	// targetEvictStake = 9 - 5 = 4. Evicting A (stake=2) alone frees 2 < 4.
	// Must evict both A (2) and B (2) to free 4 ≥ 4.
	for range 5 {
		b.RegisterFailure(vdrD)
	}
	// Expect: Unbenched(A) + Unbenched(B) + Benched(D) = 3 signals.
	<-benchable.updated
	<-benchable.updated
	<-benchable.updated

	require.False(b.IsBenched(vdrA), "A should be evicted")
	require.False(b.IsBenched(vdrB), "B should be evicted")
	require.True(b.IsBenched(vdrD), "D should be benched")
}

// TestBenchlistEvictionRefusedWhenNoBetterCandidate verifies that eviction
// does not occur when the incoming node has a lower failure probability than
// all currently benched nodes.
func TestBenchlistEvictionRefusedWhenNoBetterCandidate(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()

	// 3 validators with stake=1. maxPortion=0.4 → room for 1.
	vdrA := ids.GenerateTestNodeID()
	vdrB := ids.GenerateTestNodeID()
	vdrC := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrA, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrB, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrC, nil, ids.Empty, 1))

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
			MaxPortion:         0.4,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	defer b.shutdown()

	// Bench A with high failure probability: many failures → p close to 1.
	for range 10 {
		b.RegisterFailure(vdrA)
	}
	<-benchable.updated
	require.True(b.IsBenched(vdrA))

	// B crosses bench threshold with lower p than A.
	// 2 failures → p ≈ 0.67, which is well below A's ~0.999.
	// Eviction should NOT happen because A is worse than B.
	b.RegisterFailure(vdrB)
	b.RegisterFailure(vdrB)

	select {
	case <-benchable.updated:
		require.FailNow("unexpected bench/unbench — eviction should not occur")
	case <-time.After(50 * time.Millisecond):
	}

	require.True(b.IsBenched(vdrA), "A should remain benched")
	require.False(b.IsBenched(vdrB), "B should not be benched")
}

// TestBenchlistEvictionRespectsStakeConstraints verifies that eviction does
// not occur when evicting the candidate would not free enough stake for the
// incoming node.
func TestBenchlistEvictionRespectsStakeConstraints(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()

	// A has stake=1, B has stake=3, C has stake=6. Total = 10.
	// maxPortion = 0.15 → maxBenchedStake = 1.5.
	// A (stake=1) can be benched. B (stake=3) cannot fit even if A is evicted
	// because 3 > 1.5.
	vdrA := ids.GenerateTestNodeID()
	vdrB := ids.GenerateTestNodeID()
	vdrC := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrA, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrB, nil, ids.Empty, 3))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrC, nil, ids.Empty, 6))

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
			MaxPortion:         0.15,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	defer b.shutdown()

	// Bench A (stake=1): fits within maxBenchedStake = 1.5.
	b.RegisterFailure(vdrA)
	b.RegisterFailure(vdrA)
	<-benchable.updated
	require.True(b.IsBenched(vdrA))

	// B (stake=3) crosses bench threshold with higher p than A.
	// Even though B has higher p, evicting A frees stake=1, and
	// B needs stake=3, which exceeds maxBenchedStake=1.5.
	for range 5 {
		b.RegisterFailure(vdrB)
	}

	select {
	case <-benchable.updated:
		require.FailNow("unexpected bench/unbench — stake constraint should prevent eviction")
	case <-time.After(50 * time.Millisecond):
	}

	require.True(b.IsBenched(vdrA), "A should remain benched")
	require.False(b.IsBenched(vdrB), "B should not be benched — too much stake")
}

// TestBenchlistEvictedNodePreservesEWMA verifies that an evicted node retains
// its failure probability (EWMA is not reset), allowing organic recovery via
// the unbench threshold path.
func TestBenchlistEvictedNodePreservesEWMA(t *testing.T) {
	require := require.New(t)
	const halflife = 25 * time.Millisecond

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()

	vdrA := ids.GenerateTestNodeID()
	vdrB := ids.GenerateTestNodeID()
	vdrC := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrA, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrB, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrC, nil, ids.Empty, 1))

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
			Halflife:           halflife,
			UnbenchProbability: DefaultUnbenchProbability,
			BenchProbability:   DefaultBenchProbability,
			BenchDuration:      DefaultBenchDuration,
			MaxPortion:         0.4,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	defer b.shutdown()

	// Bench A: 2 failures → p ≈ 0.67.
	b.RegisterFailure(vdrA)
	b.RegisterFailure(vdrA)
	<-benchable.updated
	require.True(b.IsBenched(vdrA))

	// Evict A by benching B with higher p = 0.75.
	b.RegisterFailure(vdrB)
	b.RegisterFailure(vdrB)
	b.RegisterFailure(vdrB)
	<-benchable.updated // Unbenched(A)
	<-benchable.updated // Benched(B)
	require.False(b.IsBenched(vdrA))
	require.True(b.IsBenched(vdrB))

	// Bench A again with two failures → p ≈ 0.8.
	// If A's EWMA does not reset, this yields 0.8 and evicts A.
	// If A's EWMA does reset, this yields 0.67 and is insufficient to evict B.
	for range 2 {
		b.RegisterFailure(vdrA)
	}
	<-benchable.updated
	<-benchable.updated
	require.True(b.IsBenched(vdrA))
	require.False(b.IsBenched(vdrB))
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
	case <-time.After(defaultTestTimeout):
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
	case <-time.After(defaultTestTimeout):
		require.FailNow("observe path blocked while consumer was blocked")
	}

	close(benchable.unblock)
}
