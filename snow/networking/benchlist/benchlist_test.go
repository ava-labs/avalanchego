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

func (b *benchable) Unbenched(chainID ids.ID, nodeID ids.NodeID) {
	require.Equal(b.t, b.wantChainID, chainID)
	require.Contains(b.t, b.benched, nodeID)
	b.benched.Remove(nodeID)
	b.updated <- struct{}{}
}

func TestBenchlist(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID := ids.GenerateTestNodeID()
	nodeID := ids.GenerateTestNodeID()

	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))

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
		require.Zero(testutil.ToFloat64(b.numBenched))
		require.Zero(testutil.ToFloat64(b.weightBenched))
	}

	requireBenched := func() {
		t.Helper()
		require.True(b.IsBenched(vdrID))
		require.False(b.IsBenched(nodeID))
		require.Equal(1.0, testutil.ToFloat64(b.numBenched))
		require.Equal(1.0, testutil.ToFloat64(b.weightBenched))
	}

	// Nobody should be benched at the start
	requireNoBenchings()

	b.RegisterResponse(vdrID) // p = 0 / 1
	requireNoBenchings()

	b.RegisterFailure(vdrID) // p = 1 / 2
	requireNoBenchings()

	b.RegisterFailure(vdrID) // p = 2 / 3
	<-benchable.updated
	requireBenched()

	b.RegisterFailure(nodeID) // Non-validators shouldn't be tracked
	requireBenched()

	for range 8 {
		b.RegisterResponse(vdrID)
	} // p = 2 / 11
	<-benchable.updated
	requireNoBenchings()

	// p = 1 / 5.5
	now = now.Add(DefaultHalflife)
	b.clock.Set(now)

	for range 4 {
		b.RegisterFailure(vdrID)
	} // p = 5 / 9.5
	<-benchable.updated
	requireBenched()
}

func TestBenchlistTimeout(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID := ids.GenerateTestNodeID()

	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))

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
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	now := time.Now()
	b.clock.Set(now)

	// Bench the node: p = 2/3 > 0.5
	b.RegisterResponse(vdrID)
	b.RegisterFailure(vdrID)
	b.RegisterFailure(vdrID)
	<-benchable.updated
	require.True(b.IsBenched(vdrID))

	// The consumer goroutine's timer also fires (real time ≥ 50ms) and calls
	// Unbenched on the benchable.
	<-benchable.updated
	require.False(b.IsBenched(vdrID))
	require.Empty(benchable.benched)
}

// Test that when a node is unbenched via timeout, its EWMA history is wiped
// so that it gets a clean slate. Without the reset, a single failure after
// unbenching would re-bench the node because the old high failure probability
// is still present.
func TestBenchlistTimeoutCleansSlate(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID := ids.GenerateTestNodeID()

	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))

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
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	now := time.Now()
	b.clock.Set(now)

	// Bench the node: p = 2/3 > 0.5
	b.RegisterResponse(vdrID)
	b.RegisterFailure(vdrID)
	b.RegisterFailure(vdrID)
	<-benchable.updated
	require.True(b.IsBenched(vdrID))

	// Wait for timeout-based unbench.
	<-benchable.updated
	require.False(b.IsBenched(vdrID))

	// Register a response followed by a failure. With a clean slate, p = 1/2
	// which is NOT > benchProbability (0.5), so the node should NOT be
	// re-benched. Without the EWMA reset, the old high failure probability
	// would cause the node to be immediately re-benched.
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
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	// Bench the node and wait for the consumer to block in Benched.
	b.RegisterResponse(vdrID)
	b.RegisterFailure(vdrID)
	b.RegisterFailure(vdrID)
	select {
	case <-benchable.benchedCalled:
	case <-time.After(time.Second):
		require.FailNow("timed out waiting for consumer to block in Benched")
	}

	done := make(chan struct{})
	go func() {
		// Trigger two transitions while the consumer is blocked:
		// benched -> unbenched, then unbenched -> benched.
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

func TestRunDrainsEntireJobQueuePerSignal(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	vdrID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))

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
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	require.True(b.jobs.PushRight(job{
		nodeID: vdrID,
		bench:  true,
	}))
	require.True(b.jobs.PushRight(job{
		nodeID: vdrID,
		bench:  false,
	}))
	b.jobReady <- struct{}{}

	select {
	case <-benchable.updated:
	case <-time.After(time.Second):
		require.FailNow("timed out waiting for first drained job")
	}
	select {
	case <-benchable.updated:
	case <-time.After(time.Second):
		require.FailNow("timed out waiting for second drained job")
	}
}
