// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/handler/handlermock"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	commontracker "github.com/ava-labs/avalanchego/snow/engine/common/tracker"
)

const (
	engineType         = p2ppb.EngineType_ENGINE_TYPE_DAG
	testThreadPoolSize = 2
)

// TODO refactor tests in this file

func TestShutdown(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	chainCtx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(chainCtx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))
	benchlist := benchlist.NewNoBenchlist()
	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     time.Millisecond,
			MinimumTimeout:     time.Millisecond,
			MaximumTimeout:     10 * time.Second,
			TimeoutCoefficient: 1.25,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist,
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go tm.Dispatch()
	defer tm.Stop()

	chainRouter := ChainRouter{}
	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Second,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		HealthConfig{},
		prometheus.NewRegistry(),
	))

	shutdownCalled := make(chan struct{}, 1)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	p2pTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.Current,
	)
	require.NoError(err)

	h, err := handler.New(
		chainCtx,
		&block.ChangeNotifier{},
		noopSubscription,
		vdrs,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(chainCtx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		p2pTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(true)
	bootstrapper.CantGossip = false
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return chainCtx
	}
	bootstrapper.ShutdownF = func(context.Context) error {
		shutdownCalled <- struct{}{}
		return nil
	}
	bootstrapper.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}
	bootstrapper.HaltF = func(context.Context) {}

	engine := &enginetest.Engine{T: t}
	engine.Default(true)
	engine.CantGossip = false
	engine.ContextF = func() *snow.ConsensusContext {
		return chainCtx
	}
	engine.ShutdownF = func(context.Context) error {
		shutdownCalled <- struct{}{}
		return nil
	}
	engine.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}
	engine.HaltF = func(context.Context) {}
	h.SetEngineManager(&handler.EngineManager{
		DAG: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})
	chainCtx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(t.Context(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(t.Context(), false)

	chainRouter.Shutdown(t.Context())

	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()

	select {
	case <-ctx.Done():
		require.FailNow("Handler shutdown was not called or timed out after 250ms during chainRouter shutdown")
	case <-shutdownCalled:
	}

	shutdownDuration, err := h.AwaitStopped(ctx)
	require.NoError(err)
	require.GreaterOrEqual(shutdownDuration, time.Duration(0))
	require.Less(shutdownDuration, 250*time.Millisecond)
}

func TestConnectedAfterShutdownErrorLogRegression(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.PChainID)
	chainCtx := snowtest.ConsensusContext(snowCtx)

	chainRouter := ChainRouter{}
	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoWarn{}, // If an error log is emitted, the test will fail
		nil,
		time.Second,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		HealthConfig{},
		prometheus.NewRegistry(),
	))

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	p2pTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.Current,
	)
	require.NoError(err)

	h, err := handler.New(
		chainCtx,
		&block.ChangeNotifier{},
		noopSubscription,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(chainCtx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		p2pTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	engine := enginetest.Engine{
		T: t,
		StartF: func(context.Context, uint32) error {
			return nil
		},
		ContextF: func() *snow.ConsensusContext {
			return chainCtx
		},
		HaltF: func(context.Context) {},
		ShutdownF: func(context.Context) error {
			return nil
		},
		ConnectedF: func(context.Context, ids.NodeID, *version.Application) error {
			return nil
		},
	}
	engine.Default(true)
	engine.CantGossip = false

	bootstrapper := &enginetest.Bootstrapper{
		Engine:    engine,
		CantClear: true,
	}

	h.SetEngineManager(&handler.EngineManager{
		DAG: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    &engine,
		},
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    &engine,
		},
	})
	chainCtx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(t.Context(), h)

	h.Start(t.Context(), false)

	chainRouter.Shutdown(t.Context())

	shutdownDuration, err := h.AwaitStopped(t.Context())
	require.NoError(err)
	require.GreaterOrEqual(shutdownDuration, time.Duration(0))

	// Calling connected after shutdown should result in an error log.
	chainRouter.Connected(
		ids.GenerateTestNodeID(),
		version.Current,
		ids.GenerateTestID(),
	)
}

func TestShutdownTimesOut(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	nodeID := ids.EmptyNodeID
	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))
	benchlist := benchlist.NewNoBenchlist()
	// Ensure that the Ancestors request does not timeout
	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     time.Second,
			MinimumTimeout:     500 * time.Millisecond,
			MaximumTimeout:     10 * time.Second,
			TimeoutCoefficient: 1.25,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist,
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go tm.Dispatch()
	defer tm.Stop()

	chainRouter := ChainRouter{}

	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Millisecond,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		HealthConfig{},
		prometheus.NewRegistry(),
	))

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	p2pTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.Current,
	)
	require.NoError(err)

	h, err := handler.New(
		ctx,
		&block.ChangeNotifier{},
		noopSubscription,
		vdrs,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		p2pTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	bootstrapFinished := make(chan struct{}, 1)
	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(true)
	bootstrapper.CantGossip = false
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}
	bootstrapper.HaltF = func(context.Context) {}
	bootstrapper.PullQueryF = func(context.Context, ids.NodeID, uint32, ids.ID, uint64) error {
		// Ancestors blocks for two seconds
		time.Sleep(2 * time.Second)
		bootstrapFinished <- struct{}{}
		return nil
	}

	engine := &enginetest.Engine{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	closed := new(int)
	engine.ShutdownF = func(context.Context) error {
		*closed++
		return nil
	}
	h.SetEngineManager(&handler.EngineManager{
		DAG: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(t.Context(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(t.Context(), false)

	shutdownFinished := make(chan struct{}, 1)

	go func() {
		chainID := ids.Empty
		msg := handler.Message{
			InboundMessage: message.InboundPullQuery(chainID, 1, time.Hour, ids.GenerateTestID(), 0, nodeID),
			EngineType:     p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
		}
		h.Push(t.Context(), msg)

		time.Sleep(50 * time.Millisecond) // Pause to ensure message gets processed

		chainRouter.Shutdown(t.Context())
		shutdownFinished <- struct{}{}
	}()

	select {
	case <-bootstrapFinished:
		require.FailNow("Shutdown should have finished in one millisecond before timing out instead of waiting for engine to finish shutting down.")
	case <-shutdownFinished:
	}
}

// Ensure that a timeout fires if we don't get a response to a request
func TestRouterTimeout(t *testing.T) {
	require := require.New(t)

	// Create a timeout manager
	maxTimeout := 25 * time.Millisecond
	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     10 * time.Millisecond,
			MinimumTimeout:     10 * time.Millisecond,
			MaximumTimeout:     maxTimeout,
			TimeoutCoefficient: 1,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist.NewNoBenchlist(),
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go tm.Dispatch()
	defer tm.Stop()

	// Create a router
	chainRouter := ChainRouter{}
	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Millisecond,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		HealthConfig{},
		prometheus.NewRegistry(),
	))
	defer chainRouter.Shutdown(t.Context())

	// Create bootstrapper, engine and handler
	var (
		calledGetStateSummaryFrontierFailed,
		calledGetAcceptedStateSummaryFailed,
		calledGetAcceptedFrontierFailed,
		calledGetAcceptedFailed,
		calledGetAncestorsFailed,
		calledGetFailed,
		calledQueryFailed,
		calledAppRequestFailed bool

		wg = sync.WaitGroup{}
	)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	p2pTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.Current,
	)
	require.NoError(err)

	h, err := handler.New(
		ctx,
		&block.ChangeNotifier{},
		noopSubscription,
		vdrs,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		p2pTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(true)
	bootstrapper.CantGossip = false
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}
	bootstrapper.HaltF = func(context.Context) {}
	bootstrapper.ShutdownF = func(context.Context) error { return nil }

	bootstrapper.GetStateSummaryFrontierFailedF = func(context.Context, ids.NodeID, uint32) error {
		defer wg.Done()
		calledGetStateSummaryFrontierFailed = true
		return nil
	}
	bootstrapper.GetAcceptedStateSummaryFailedF = func(context.Context, ids.NodeID, uint32) error {
		defer wg.Done()
		calledGetAcceptedStateSummaryFailed = true
		return nil
	}
	bootstrapper.GetAcceptedFrontierFailedF = func(context.Context, ids.NodeID, uint32) error {
		defer wg.Done()
		calledGetAcceptedFrontierFailed = true
		return nil
	}
	bootstrapper.GetAncestorsFailedF = func(context.Context, ids.NodeID, uint32) error {
		defer wg.Done()
		calledGetAncestorsFailed = true
		return nil
	}
	bootstrapper.GetAcceptedFailedF = func(context.Context, ids.NodeID, uint32) error {
		defer wg.Done()
		calledGetAcceptedFailed = true
		return nil
	}
	bootstrapper.GetFailedF = func(context.Context, ids.NodeID, uint32) error {
		defer wg.Done()
		calledGetFailed = true
		return nil
	}
	bootstrapper.QueryFailedF = func(context.Context, ids.NodeID, uint32) error {
		defer wg.Done()
		calledQueryFailed = true
		return nil
	}
	bootstrapper.AppRequestFailedF = func(context.Context, ids.NodeID, uint32, *common.AppError) error {
		defer wg.Done()
		calledAppRequestFailed = true
		return nil
	}
	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.Bootstrapping, // assumed bootstrapping is ongoing
	})

	chainRouter.AddChain(t.Context(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.SetEngineManager(&handler.EngineManager{
		DAG: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
	})
	h.Start(t.Context(), false)

	nodeID := ids.GenerateTestNodeID()
	requestID := uint32(0)
	{
		wg.Add(1)
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.StateSummaryFrontierOp,
			message.InternalGetStateSummaryFrontierFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.AcceptedStateSummaryOp,
			message.InternalGetAcceptedStateSummaryFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.AcceptedFrontierOp,
			message.InternalGetAcceptedFrontierFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.AcceptedOp,
			message.InternalGetAcceptedFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.AncestorsOp,
			message.InternalGetAncestorsFailed(
				nodeID,
				ctx.ChainID,
				requestID,
				p2ppb.EngineType_ENGINE_TYPE_CHAIN,
			),
			p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.PutOp,
			message.InternalGetFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.ChitsOp,
			message.InternalQueryFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.AppResponseOp,
			message.InboundAppError(
				nodeID,
				ctx.ChainID,
				requestID,
				common.ErrTimeout.Code,
				common.ErrTimeout.Message,
			),
			p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		)
	}

	wg.Wait()

	chainRouter.lock.Lock()
	defer chainRouter.lock.Unlock()

	require.True(calledGetStateSummaryFrontierFailed)
	require.True(calledGetAcceptedStateSummaryFailed)
	require.True(calledGetAcceptedFrontierFailed)
	require.True(calledGetAcceptedFailed)
	require.True(calledGetAncestorsFailed)
	require.True(calledGetFailed)
	require.True(calledQueryFailed)
	require.True(calledAppRequestFailed)
}

func TestRouterHonorsRequestedEngine(t *testing.T) {
	ctrl := gomock.NewController(t)
	require := require.New(t)

	// Create a timeout manager
	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     3 * time.Second,
			MinimumTimeout:     3 * time.Second,
			MaximumTimeout:     5 * time.Minute,
			TimeoutCoefficient: 1,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist.NewNoBenchlist(),
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go tm.Dispatch()
	defer tm.Stop()

	// Create a router
	chainRouter := ChainRouter{}
	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Millisecond,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		HealthConfig{},
		prometheus.NewRegistry(),
	))
	defer chainRouter.Shutdown(t.Context())

	h := handlermock.NewHandler(ctrl)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	h.EXPECT().Context().Return(ctx).AnyTimes()
	h.EXPECT().SetOnStopped(gomock.Any()).AnyTimes()
	h.EXPECT().Stop(gomock.Any()).AnyTimes()
	h.EXPECT().AwaitStopped(gomock.Any()).AnyTimes()

	h.EXPECT().Push(gomock.Any(), gomock.Any()).Times(1)
	chainRouter.AddChain(t.Context(), h)

	h.EXPECT().ShouldHandle(gomock.Any()).Return(true).AnyTimes()

	nodeID := ids.GenerateTestNodeID()
	requestID := uint32(0)
	{
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.StateSummaryFrontierOp,
			message.InternalGetStateSummaryFrontierFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
		msg := message.InboundStateSummaryFrontier(
			ctx.ChainID,
			requestID,
			nil,
			nodeID,
		)

		h.EXPECT().Push(gomock.Any(), gomock.Any()).Do(func(_ context.Context, msg handler.Message) {
			require.Equal(p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED, msg.EngineType)
		})
		chainRouter.HandleInbound(t.Context(), msg)
	}

	{
		requestID++
		chainRouter.RegisterRequest(
			t.Context(),
			nodeID,
			ctx.ChainID,
			requestID,
			message.AcceptedStateSummaryOp,
			message.InternalGetAcceptedStateSummaryFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			engineType,
		)
		msg := message.InboundAcceptedStateSummary(
			ctx.ChainID,
			requestID,
			nil,
			nodeID,
		)

		h.EXPECT().Push(gomock.Any(), gomock.Any()).Do(func(_ context.Context, msg handler.Message) {
			require.Equal(engineType, msg.EngineType)
		})
		chainRouter.HandleInbound(t.Context(), msg)
	}

	{
		requestID++
		msg := message.InboundPushQuery(
			ctx.ChainID,
			requestID,
			0,
			nil,
			0,
			nodeID,
		)

		h.EXPECT().Push(gomock.Any(), gomock.Any()).Do(func(_ context.Context, msg handler.Message) {
			require.Equal(p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED, msg.EngineType)
		})
		chainRouter.HandleInbound(t.Context(), msg)
	}

	chainRouter.lock.Lock()
	require.Zero(chainRouter.timedRequests.Len())
	chainRouter.lock.Unlock()
}

func TestRouterClearTimeouts(t *testing.T) {
	requestID := uint32(123)

	tests := []struct {
		name        string
		responseOp  message.Op
		responseMsg *message.InboundMessage
		timeoutMsg  *message.InboundMessage
	}{
		{
			name:        "StateSummaryFrontier",
			responseOp:  message.StateSummaryFrontierOp,
			responseMsg: message.InboundStateSummaryFrontier(ids.Empty, requestID, []byte("summary"), ids.EmptyNodeID),
			timeoutMsg:  message.InternalGetStateSummaryFrontierFailed(ids.EmptyNodeID, ids.Empty, requestID),
		},
		{
			name:        "AcceptedStateSummary",
			responseOp:  message.AcceptedStateSummaryOp,
			responseMsg: message.InboundAcceptedStateSummary(ids.Empty, requestID, []ids.ID{ids.GenerateTestID()}, ids.EmptyNodeID),
			timeoutMsg:  message.InternalGetAcceptedStateSummaryFailed(ids.EmptyNodeID, ids.Empty, requestID),
		},
		{
			name:        "AcceptedFrontierOp",
			responseOp:  message.AcceptedFrontierOp,
			responseMsg: message.InboundAcceptedFrontier(ids.Empty, requestID, ids.GenerateTestID(), ids.EmptyNodeID),
			timeoutMsg:  message.InternalGetAcceptedFrontierFailed(ids.EmptyNodeID, ids.Empty, requestID),
		},
		{
			name:        "Accepted",
			responseOp:  message.AcceptedOp,
			responseMsg: message.InboundAccepted(ids.Empty, requestID, []ids.ID{ids.GenerateTestID()}, ids.EmptyNodeID),
			timeoutMsg:  message.InternalGetAcceptedFailed(ids.EmptyNodeID, ids.Empty, requestID),
		},
		{
			name:        "Chits",
			responseOp:  message.ChitsOp,
			responseMsg: message.InboundChits(ids.Empty, requestID, ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID(), ids.EmptyNodeID),
			timeoutMsg:  message.InternalQueryFailed(ids.EmptyNodeID, ids.Empty, requestID),
		},
		{
			name:        "AppResponse",
			responseOp:  message.AppResponseOp,
			responseMsg: message.InboundAppResponse(ids.Empty, requestID, []byte("responseMsg"), ids.EmptyNodeID),
			timeoutMsg:  message.InboundAppError(ids.EmptyNodeID, ids.Empty, requestID, 123, "error"),
		},
		{
			name:        "AppError",
			responseOp:  message.AppResponseOp,
			responseMsg: message.InboundAppError(ids.EmptyNodeID, ids.Empty, requestID, 1234, "custom error"),
			timeoutMsg:  message.InboundAppError(ids.EmptyNodeID, ids.Empty, requestID, 123, "error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			chainRouter, _ := newChainRouterTest(t)

			chainRouter.RegisterRequest(
				t.Context(),
				ids.EmptyNodeID,
				ids.Empty,
				requestID,
				tt.responseOp,
				tt.timeoutMsg,
				engineType,
			)

			chainRouter.HandleInbound(t.Context(), tt.responseMsg)

			chainRouter.lock.Lock()
			require.Zero(chainRouter.timedRequests.Len())
			chainRouter.lock.Unlock()
		})
	}
}

func TestValidatorOnlyMessageDrops(t *testing.T) {
	require := require.New(t)

	// Create a timeout manager
	maxTimeout := 25 * time.Millisecond
	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     10 * time.Millisecond,
			MinimumTimeout:     10 * time.Millisecond,
			MaximumTimeout:     maxTimeout,
			TimeoutCoefficient: 1,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist.NewNoBenchlist(),
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go tm.Dispatch()
	defer tm.Stop()

	// Create a router
	chainRouter := ChainRouter{}
	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Millisecond,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		HealthConfig{},
		prometheus.NewRegistry(),
	))
	defer chainRouter.Shutdown(t.Context())

	// Create bootstrapper, engine and handler
	calledF := false
	wg := sync.WaitGroup{}

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	sb := subnets.New(ctx.NodeID, subnets.Config{ValidatorOnly: true})
	vdrs := validators.NewManager()
	vID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vID, nil, ids.Empty, 1))
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	p2pTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.Current,
	)
	require.NoError(err)

	h, err := handler.New(
		ctx,
		&block.ChangeNotifier{},
		noopSubscription,
		vdrs,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		sb,
		commontracker.NewPeers(),
		p2pTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.PullQueryF = func(context.Context, ids.NodeID, uint32, ids.ID, uint64) error {
		defer wg.Done()
		calledF = true
		return nil
	}
	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.Bootstrapping, // assumed bootstrapping is ongoing
	})

	engine := &enginetest.Engine{T: t}
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	engine.Default(false)
	h.SetEngineManager(&handler.EngineManager{
		DAG: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	chainRouter.AddChain(t.Context(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(t.Context(), false)

	var inMsg *message.InboundMessage
	dummyContainerID := ids.GenerateTestID()
	reqID := uint32(0)

	// Non-validator case
	nID := ids.GenerateTestNodeID()

	calledF = false
	inMsg = message.InboundPullQuery(
		ctx.ChainID,
		reqID,
		time.Hour,
		dummyContainerID,
		0,
		nID,
	)
	chainRouter.HandleInbound(t.Context(), inMsg)

	require.False(calledF) // should not be called

	// Validator case
	calledF = false
	reqID++
	inMsg = message.InboundPullQuery(
		ctx.ChainID,
		reqID,
		time.Hour,
		dummyContainerID,
		0,
		vID,
	)
	wg.Add(1)
	chainRouter.HandleInbound(t.Context(), inMsg)

	wg.Wait()
	require.True(calledF) // should be called since this is a validator request
}

func TestValidatorOnlyAllowedNodeMessageDrops(t *testing.T) {
	require := require.New(t)

	// Create a timeout manager
	maxTimeout := 25 * time.Millisecond
	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     10 * time.Millisecond,
			MinimumTimeout:     10 * time.Millisecond,
			MaximumTimeout:     maxTimeout,
			TimeoutCoefficient: 1,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist.NewNoBenchlist(),
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go tm.Dispatch()
	defer tm.Stop()

	// Create a router
	chainRouter := ChainRouter{}
	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Millisecond,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		HealthConfig{},
		prometheus.NewRegistry(),
	))
	defer chainRouter.Shutdown(t.Context())

	// Create bootstrapper, engine and handler
	calledF := false
	wg := sync.WaitGroup{}

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	allowedID := ids.GenerateTestNodeID()
	allowedSet := set.Of(allowedID)
	sb := subnets.New(ctx.NodeID, subnets.Config{ValidatorOnly: true, AllowedNodes: allowedSet})

	vdrs := validators.NewManager()
	vID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vID, nil, ids.Empty, 1))

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	p2pTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.Current,
	)
	require.NoError(err)

	h, err := handler.New(
		ctx,
		&block.ChangeNotifier{},
		noopSubscription,
		vdrs,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		sb,
		commontracker.NewPeers(),
		p2pTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.PullQueryF = func(context.Context, ids.NodeID, uint32, ids.ID, uint64) error {
		defer wg.Done()
		calledF = true
		return nil
	}
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.Bootstrapping, // assumed bootstrapping is ongoing
	})
	engine := &enginetest.Engine{T: t}
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	engine.Default(false)

	h.SetEngineManager(&handler.EngineManager{
		DAG: &handler.Engine{
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	chainRouter.AddChain(t.Context(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(t.Context(), false)

	var inMsg *message.InboundMessage
	dummyContainerID := ids.GenerateTestID()
	reqID := uint32(0)

	// Non-validator case
	nID := ids.GenerateTestNodeID()

	calledF = false
	inMsg = message.InboundPullQuery(
		ctx.ChainID,
		reqID,
		time.Hour,
		dummyContainerID,
		0,
		nID,
	)
	chainRouter.HandleInbound(t.Context(), inMsg)

	require.False(calledF) // should not be called for unallowed node ID

	// Allowed NodeID case
	calledF = false
	reqID++
	inMsg = message.InboundPullQuery(
		ctx.ChainID,
		reqID,
		time.Hour,
		dummyContainerID,
		0,
		allowedID,
	)
	wg.Add(1)
	chainRouter.HandleInbound(t.Context(), inMsg)

	wg.Wait()
	require.True(calledF) // should be called since this is a allowed node request

	// Validator case
	calledF = false
	reqID++
	inMsg = message.InboundPullQuery(
		ctx.ChainID,
		reqID,
		time.Hour,
		dummyContainerID,
		0,
		vID,
	)
	wg.Add(1)
	chainRouter.HandleInbound(t.Context(), inMsg)

	wg.Wait()
	require.True(calledF) // should be called since this is a validator request
}

// Tests that a response, peer error, or a timeout clears the timeout and calls
// the handler
func TestAppRequest(t *testing.T) {
	wantRequestID := uint32(123)
	wantResponse := []byte("response")

	errFoo := common.AppError{
		Code:    456,
		Message: "foo",
	}

	tests := []struct {
		name       string
		responseOp message.Op
		timeoutMsg *message.InboundMessage
		inboundMsg *message.InboundMessage
	}{
		{
			name:       "AppRequest - chain response",
			responseOp: message.AppResponseOp,
			timeoutMsg: message.InboundAppError(ids.EmptyNodeID, ids.Empty, wantRequestID, errFoo.Code, errFoo.Message),
			inboundMsg: message.InboundAppResponse(ids.Empty, wantRequestID, wantResponse, ids.EmptyNodeID),
		},
		{
			name:       "AppRequest - chain error",
			responseOp: message.AppResponseOp,
			timeoutMsg: message.InboundAppError(ids.EmptyNodeID, ids.Empty, wantRequestID, errFoo.Code, errFoo.Message),
			inboundMsg: message.InboundAppError(ids.EmptyNodeID, ids.Empty, wantRequestID, errFoo.Code, errFoo.Message),
		},
		{
			name:       "AppRequest - timeout",
			responseOp: message.AppResponseOp,
			timeoutMsg: message.InboundAppError(ids.EmptyNodeID, ids.Empty, wantRequestID, errFoo.Code, errFoo.Message),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			wg := &sync.WaitGroup{}
			chainRouter, engine := newChainRouterTest(t)

			wg.Add(1)
			if tt.inboundMsg == nil || tt.inboundMsg.Op == message.AppErrorOp {
				engine.AppRequestFailedF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
					defer wg.Done()
					chainRouter.lock.Lock()
					require.Zero(chainRouter.timedRequests.Len())
					chainRouter.lock.Unlock()

					require.Equal(ids.EmptyNodeID, nodeID)
					require.Equal(wantRequestID, requestID)
					require.Equal(errFoo.Code, appErr.Code)
					require.Equal(errFoo.Message, appErr.Message)

					return nil
				}
			} else if tt.inboundMsg.Op == message.AppResponseOp {
				engine.AppResponseF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
					defer wg.Done()
					chainRouter.lock.Lock()
					require.Zero(chainRouter.timedRequests.Len())
					chainRouter.lock.Unlock()

					require.Equal(ids.EmptyNodeID, nodeID)
					require.Equal(wantRequestID, requestID)
					require.Equal(wantResponse, msg)

					return nil
				}
			}

			ctx := t.Context()
			chainRouter.RegisterRequest(ctx, ids.EmptyNodeID, ids.Empty, wantRequestID, tt.responseOp, tt.timeoutMsg, engineType)
			chainRouter.lock.Lock()
			require.Equal(1, chainRouter.timedRequests.Len())
			chainRouter.lock.Unlock()

			if tt.inboundMsg != nil {
				chainRouter.HandleInbound(ctx, tt.inboundMsg)
			}

			wg.Wait()
		})
	}
}

func newChainRouterTest(t *testing.T) (*ChainRouter, *enginetest.Engine) {
	// Create a timeout manager
	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     3 * time.Second,
			MinimumTimeout:     3 * time.Second,
			MaximumTimeout:     5 * time.Minute,
			TimeoutCoefficient: 1,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist.NewNoBenchlist(),
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	go tm.Dispatch()

	// Create a router
	chainRouter := &ChainRouter{}
	require.NoError(t, chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Millisecond,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		HealthConfig{},
		prometheus.NewRegistry(),
	))

	// Create bootstrapper, engine and handler
	snowCtx := snowtest.Context(t, snowtest.PChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	require.NoError(t, vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)

	p2pTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.Current,
	)
	require.NoError(t, err)

	h, err := handler.New(
		ctx,
		&block.ChangeNotifier{},
		noopSubscription,
		vdrs,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		p2pTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(t, err)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}

	engine := &enginetest.Engine{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	h.SetEngineManager(&handler.EngineManager{
		DAG: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})
	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(t.Context(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	h.Start(t.Context(), false)

	t.Cleanup(func() {
		tm.Stop()
		chainRouter.Shutdown(t.Context())
	})

	return chainRouter, engine
}

// Tests that HandleInbound correctly handles Simplex Messages
func TestHandleSimplexMessage(t *testing.T) {
	chainRouter := ChainRouter{}
	log := tests.NewDefaultLogger("test")
	log.SetLevel(logging.Debug)
	require.NoError(t,
		chainRouter.Initialize(
			ids.EmptyNodeID,
			log,
			nil,
			time.Second,
			set.Set[ids.ID]{},
			true,
			set.Set[ids.ID]{},
			nil,
			HealthConfig{},
			prometheus.NewRegistry(),
		))
	defer chainRouter.Shutdown(t.Context())

	chainRouter.log = log
	testID := ids.GenerateTestID()

	msg := &p2ppb.Simplex{
		Message: &p2ppb.Simplex_ReplicationRequest{
			ReplicationRequest: &p2ppb.ReplicationRequest{
				Seqs:        []uint64{1, 2, 3},
				LatestRound: 1,
			},
		},
		ChainId: testID[:],
	}

	inboundMsg := message.InboundSimplexMessage(ids.NodeID{}, msg)

	ctrl := gomock.NewController(t)
	h := handlermock.NewHandler(ctrl)
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	ctx.ChainID = testID
	h.EXPECT().Context().Return(ctx).AnyTimes()
	h.EXPECT().SetOnStopped(gomock.Any()).AnyTimes()
	h.EXPECT().Stop(gomock.Any()).AnyTimes()
	h.EXPECT().AwaitStopped(gomock.Any()).AnyTimes()

	var receivedMsg bool
	h.EXPECT().Push(gomock.Any(), gomock.Any()).
		Do(func(_ context.Context, msg handler.Message) {
			if msg.Op == message.SimplexOp {
				receivedMsg = true
			}
		}).AnyTimes()

	chainRouter.AddChain(t.Context(), h)
	h.EXPECT().ShouldHandle(gomock.Any()).Return(true).Times(1)
	chainRouter.HandleInbound(t.Context(), inboundMsg)
	require.True(t, receivedMsg)
}

func noopSubscription(ctx context.Context) (common.Message, error) {
	<-ctx.Done()
	return common.Message(0), ctx.Err()
}
