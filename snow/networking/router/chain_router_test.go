// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"

	commontracker "github.com/ava-labs/avalanchego/snow/engine/common/tracker"
)

const (
	engineType         = p2p.EngineType_ENGINE_TYPE_AVALANCHE
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
		"",
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
		"",
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
	h, err := handler.New(
		chainCtx,
		vdrs,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(chainCtx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
	)
	require.NoError(err)

	bootstrapper := &common.BootstrapperTest{
		EngineTest: common.EngineTest{
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

	engine := &common.EngineTest{T: t}
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
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})
	chainCtx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(context.Background(), false)

	chainRouter.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
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

func TestShutdownTimesOut(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	nodeID := ids.EmptyNodeID
	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))
	benchlist := benchlist.NewNoBenchlist()
	metrics := prometheus.NewRegistry()
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
		"",
		metrics,
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
		"",
		metrics,
	))

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)
	h, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
	)
	require.NoError(err)

	bootstrapFinished := make(chan struct{}, 1)
	bootstrapper := &common.BootstrapperTest{
		EngineTest: common.EngineTest{
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

	engine := &common.EngineTest{T: t}
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
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(context.Background(), false)

	shutdownFinished := make(chan struct{}, 1)

	go func() {
		chainID := ids.ID{}
		msg := handler.Message{
			InboundMessage: message.InboundPullQuery(chainID, 1, time.Hour, ids.GenerateTestID(), 0, nodeID, engineType),
			EngineType:     engineType,
		}
		h.Push(context.Background(), msg)

		time.Sleep(50 * time.Millisecond) // Pause to ensure message gets processed

		chainRouter.Shutdown(context.Background())
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
		"",
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
		"",
		prometheus.NewRegistry(),
	))
	defer chainRouter.Shutdown(context.Background())

	// Create bootstrapper, engine and handler
	var (
		calledGetStateSummaryFrontierFailed,
		calledGetAcceptedStateSummaryFailed,
		calledGetAcceptedFrontierFailed,
		calledGetAcceptedFailed,
		calledGetAncestorsFailed,
		calledGetFailed,
		calledQueryFailed,
		calledAppRequestFailed,
		calledCrossChainAppRequestFailed bool

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

	h, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
	)
	require.NoError(err)

	bootstrapper := &common.BootstrapperTest{
		EngineTest: common.EngineTest{
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
	bootstrapper.CrossChainAppRequestFailedF = func(context.Context, ids.ID, uint32, *common.AppError) error {
		defer wg.Done()
		calledCrossChainAppRequestFailed = true
		return nil
	}
	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping, // assumed bootstrapping is ongoing
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.SetEngineManager(&handler.EngineManager{
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
	})
	h.Start(context.Background(), false)

	nodeID := ids.GenerateTestNodeID()
	requestID := uint32(0)
	{
		wg.Add(1)
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			message.StateSummaryFrontierOp,
			message.InternalGetStateSummaryFrontierFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			message.AcceptedStateSummaryOp,
			message.InternalGetAcceptedStateSummaryFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			message.AcceptedFrontierOp,
			message.InternalGetAcceptedFrontierFailed(
				nodeID,
				ctx.ChainID,
				requestID,
				p2p.EngineType_ENGINE_TYPE_SNOWMAN,
			),
			p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			message.AcceptedOp,
			message.InternalGetAcceptedFailed(
				nodeID,
				ctx.ChainID,
				requestID,
				p2p.EngineType_ENGINE_TYPE_SNOWMAN,
			),
			p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			message.AncestorsOp,
			message.InternalGetAncestorsFailed(
				nodeID,
				ctx.ChainID,
				requestID,
				p2p.EngineType_ENGINE_TYPE_SNOWMAN,
			),
			p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			message.PutOp,
			message.InternalGetFailed(
				nodeID,
				ctx.ChainID,
				requestID,
				p2p.EngineType_ENGINE_TYPE_SNOWMAN,
			),
			p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			message.ChitsOp,
			message.InternalQueryFailed(
				nodeID,
				ctx.ChainID,
				requestID,
				p2p.EngineType_ENGINE_TYPE_SNOWMAN,
			),
			p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
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
			p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		)
	}

	{
		wg.Add(1)
		requestID++
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			message.CrossChainAppResponseOp,
			message.InternalCrossChainAppError(
				nodeID,
				ctx.ChainID,
				ctx.ChainID,
				requestID,
				common.ErrTimeout.Code,
				common.ErrTimeout.Message,
			),
			p2p.EngineType_ENGINE_TYPE_SNOWMAN,
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
	require.True(calledCrossChainAppRequestFailed)
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
		"",
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
		"",
		prometheus.NewRegistry(),
	))
	defer chainRouter.Shutdown(context.Background())

	h := handler.NewMockHandler(ctrl)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	h.EXPECT().Context().Return(ctx).AnyTimes()
	h.EXPECT().SetOnStopped(gomock.Any()).AnyTimes()
	h.EXPECT().Stop(gomock.Any()).AnyTimes()
	h.EXPECT().AwaitStopped(gomock.Any()).AnyTimes()

	h.EXPECT().Push(gomock.Any(), gomock.Any()).Times(1)
	chainRouter.AddChain(context.Background(), h)

	h.EXPECT().ShouldHandle(gomock.Any()).Return(true).AnyTimes()

	nodeID := ids.GenerateTestNodeID()
	requestID := uint32(0)
	{
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			message.StateSummaryFrontierOp,
			message.InternalGetStateSummaryFrontierFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
		msg := message.InboundStateSummaryFrontier(
			ctx.ChainID,
			requestID,
			nil,
			nodeID,
		)

		h.EXPECT().Push(gomock.Any(), gomock.Any()).Do(func(_ context.Context, msg handler.Message) {
			require.Equal(p2p.EngineType_ENGINE_TYPE_UNSPECIFIED, msg.EngineType)
		})
		chainRouter.HandleInbound(context.Background(), msg)
	}

	{
		requestID++
		chainRouter.RegisterRequest(
			context.Background(),
			nodeID,
			ctx.ChainID,
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
		chainRouter.HandleInbound(context.Background(), msg)
	}

	{
		engineType := p2p.EngineType(100)

		requestID++
		msg := message.InboundPushQuery(
			ctx.ChainID,
			requestID,
			0,
			nil,
			0,
			nodeID,
			engineType,
		)

		h.EXPECT().Push(gomock.Any(), gomock.Any()).Do(func(_ context.Context, msg handler.Message) {
			require.Equal(engineType, msg.EngineType)
		})
		chainRouter.HandleInbound(context.Background(), msg)
	}

	require.Zero(chainRouter.timedRequests.Len())
}

func TestRouterClearTimeouts(t *testing.T) {
	requestID := uint32(123)

	tests := []struct {
		name        string
		responseOp  message.Op
		responseMsg message.InboundMessage
		timeoutMsg  message.InboundMessage
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
			timeoutMsg:  message.InternalGetAcceptedFrontierFailed(ids.EmptyNodeID, ids.Empty, requestID, engineType),
		},
		{
			name:        "Accepted",
			responseOp:  message.AcceptedOp,
			responseMsg: message.InboundAccepted(ids.Empty, requestID, []ids.ID{ids.GenerateTestID()}, ids.EmptyNodeID),
			timeoutMsg:  message.InternalGetAcceptedFailed(ids.EmptyNodeID, ids.Empty, requestID, engineType),
		},
		{
			name:        "Chits",
			responseOp:  message.ChitsOp,
			responseMsg: message.InboundChits(ids.Empty, requestID, ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID(), ids.EmptyNodeID),
			timeoutMsg:  message.InternalQueryFailed(ids.EmptyNodeID, ids.Empty, requestID, engineType),
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
		{
			name:        "CrossChainAppResponse",
			responseOp:  message.CrossChainAppResponseOp,
			responseMsg: message.InternalCrossChainAppResponse(ids.EmptyNodeID, ids.Empty, ids.Empty, requestID, []byte("responseMsg")),
			timeoutMsg:  message.InternalCrossChainAppError(ids.EmptyNodeID, ids.Empty, ids.Empty, requestID, 123, "error"),
		},
		{
			name:        "CrossChainAppError",
			responseOp:  message.CrossChainAppResponseOp,
			responseMsg: message.InternalCrossChainAppError(ids.EmptyNodeID, ids.Empty, ids.Empty, requestID, 1234, "custom error"),
			timeoutMsg:  message.InternalCrossChainAppError(ids.EmptyNodeID, ids.Empty, ids.Empty, requestID, 123, "error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			chainRouter, _ := newChainRouterTest(t)

			chainRouter.RegisterRequest(
				context.Background(),
				ids.EmptyNodeID,
				ids.Empty,
				ids.Empty,
				requestID,
				tt.responseOp,
				tt.timeoutMsg,
				engineType,
			)

			chainRouter.HandleInbound(context.Background(), tt.responseMsg)
			require.Zero(chainRouter.timedRequests.Len())
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
		"",
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
		"",
		prometheus.NewRegistry(),
	))
	defer chainRouter.Shutdown(context.Background())

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
	h, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		sb,
		commontracker.NewPeers(),
	)
	require.NoError(err)

	bootstrapper := &common.BootstrapperTest{
		EngineTest: common.EngineTest{
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
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping, // assumed bootstrapping is ongoing
	})

	engine := &common.EngineTest{T: t}
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	engine.Default(false)
	h.SetEngineManager(&handler.EngineManager{
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(context.Background(), false)

	var inMsg message.InboundMessage
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
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	)
	chainRouter.HandleInbound(context.Background(), inMsg)

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
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	)
	wg.Add(1)
	chainRouter.HandleInbound(context.Background(), inMsg)

	wg.Wait()
	require.True(calledF) // should be called since this is a validator request
}

func TestConnectedSubnet(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     3 * time.Second,
			MinimumTimeout:     3 * time.Second,
			MaximumTimeout:     5 * time.Minute,
			TimeoutCoefficient: 1,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist.NewNoBenchlist(),
		"timeoutManager",
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go tm.Dispatch()
	defer tm.Stop()

	// Create chain router
	myNodeID := ids.GenerateTestNodeID()
	peerNodeID := ids.GenerateTestNodeID()
	subnetID0 := ids.GenerateTestID()
	subnetID1 := ids.GenerateTestID()
	trackedSubnets := set.Of(subnetID0, subnetID1)
	chainRouter := ChainRouter{}
	require.NoError(chainRouter.Initialize(
		myNodeID,
		logging.NoLog{},
		tm,
		time.Millisecond,
		set.Set[ids.ID]{},
		true,
		trackedSubnets,
		nil,
		HealthConfig{},
		"",
		prometheus.NewRegistry(),
	))

	// Create bootstrapper, engine and handler
	snowCtx := snowtest.Context(t, snowtest.PChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	ctx.Executing.Set(false)
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp,
	})

	myConnectedMsg := handler.Message{
		InboundMessage: message.InternalConnected(myNodeID, version.CurrentApp),
		EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	mySubnetConnectedMsg0 := handler.Message{
		InboundMessage: message.InternalConnectedSubnet(myNodeID, subnetID0),
		EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	mySubnetConnectedMsg1 := handler.Message{
		InboundMessage: message.InternalConnectedSubnet(myNodeID, subnetID1),
		EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}

	platformHandler := handler.NewMockHandler(ctrl)
	platformHandler.EXPECT().Context().Return(ctx).AnyTimes()
	platformHandler.EXPECT().SetOnStopped(gomock.Any()).AnyTimes()
	platformHandler.EXPECT().Push(gomock.Any(), myConnectedMsg).Times(1)
	platformHandler.EXPECT().Push(gomock.Any(), mySubnetConnectedMsg0).Times(1)
	platformHandler.EXPECT().Push(gomock.Any(), mySubnetConnectedMsg1).Times(1)

	chainRouter.AddChain(context.Background(), platformHandler)

	peerConnectedMsg := handler.Message{
		InboundMessage: message.InternalConnected(peerNodeID, version.CurrentApp),
		EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	platformHandler.EXPECT().Push(gomock.Any(), peerConnectedMsg).Times(1)
	chainRouter.Connected(peerNodeID, version.CurrentApp, constants.PrimaryNetworkID)

	peerSubnetConnectedMsg0 := handler.Message{
		InboundMessage: message.InternalConnectedSubnet(peerNodeID, subnetID0),
		EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	platformHandler.EXPECT().Push(gomock.Any(), peerSubnetConnectedMsg0).Times(1)
	chainRouter.Connected(peerNodeID, version.CurrentApp, subnetID0)

	myDisconnectedMsg := handler.Message{
		InboundMessage: message.InternalDisconnected(myNodeID),
		EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	platformHandler.EXPECT().Push(gomock.Any(), myDisconnectedMsg).Times(1)
	chainRouter.Benched(constants.PlatformChainID, myNodeID)

	peerDisconnectedMsg := handler.Message{
		InboundMessage: message.InternalDisconnected(peerNodeID),
		EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	platformHandler.EXPECT().Push(gomock.Any(), peerDisconnectedMsg).Times(1)
	chainRouter.Benched(constants.PlatformChainID, peerNodeID)

	platformHandler.EXPECT().Push(gomock.Any(), myConnectedMsg).Times(1)
	platformHandler.EXPECT().Push(gomock.Any(), mySubnetConnectedMsg0).Times(1)
	platformHandler.EXPECT().Push(gomock.Any(), mySubnetConnectedMsg1).Times(1)

	chainRouter.Unbenched(constants.PlatformChainID, myNodeID)

	platformHandler.EXPECT().Push(gomock.Any(), peerConnectedMsg).Times(1)
	platformHandler.EXPECT().Push(gomock.Any(), peerSubnetConnectedMsg0).Times(1)

	chainRouter.Unbenched(constants.PlatformChainID, peerNodeID)

	platformHandler.EXPECT().Push(gomock.Any(), peerDisconnectedMsg).Times(1)
	chainRouter.Disconnected(peerNodeID)
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
		"",
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
		"",
		prometheus.NewRegistry(),
	))
	defer chainRouter.Shutdown(context.Background())

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

	h, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		sb,
		commontracker.NewPeers(),
	)
	require.NoError(err)

	bootstrapper := &common.BootstrapperTest{
		EngineTest: common.EngineTest{
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
	engine := &common.EngineTest{T: t}
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	engine.Default(false)

	h.SetEngineManager(&handler.EngineManager{
		Avalanche: &handler.Engine{
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(context.Background(), false)

	var inMsg message.InboundMessage
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
		engineType,
	)
	chainRouter.HandleInbound(context.Background(), inMsg)

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
		engineType,
	)
	wg.Add(1)
	chainRouter.HandleInbound(context.Background(), inMsg)

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
		engineType,
	)
	wg.Add(1)
	chainRouter.HandleInbound(context.Background(), inMsg)

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
		timeoutMsg message.InboundMessage
		inboundMsg message.InboundMessage
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
			if tt.inboundMsg == nil || tt.inboundMsg.Op() == message.AppErrorOp {
				engine.AppRequestFailedF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
					defer wg.Done()
					require.Zero(chainRouter.timedRequests.Len())

					require.Equal(ids.EmptyNodeID, nodeID)
					require.Equal(wantRequestID, requestID)
					require.Equal(errFoo.Code, appErr.Code)
					require.Equal(errFoo.Message, appErr.Message)

					return nil
				}
			} else if tt.inboundMsg.Op() == message.AppResponseOp {
				engine.AppResponseF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
					defer wg.Done()
					require.Zero(chainRouter.timedRequests.Len())

					require.Equal(ids.EmptyNodeID, nodeID)
					require.Equal(wantRequestID, requestID)
					require.Equal(wantResponse, msg)

					return nil
				}
			}

			ctx := context.Background()
			chainRouter.RegisterRequest(ctx, ids.EmptyNodeID, ids.Empty, ids.Empty, wantRequestID, tt.responseOp, tt.timeoutMsg, engineType)
			require.Equal(1, chainRouter.timedRequests.Len())

			if tt.inboundMsg != nil {
				chainRouter.HandleInbound(ctx, tt.inboundMsg)
			}

			wg.Wait()
		})
	}
}

// Tests that a response, peer error, or a timeout clears the timeout and calls
// the handler
func TestCrossChainAppRequest(t *testing.T) {
	wantRequestID := uint32(123)
	wantResponse := []byte("response")

	errFoo := common.AppError{
		Code:    456,
		Message: "foo",
	}

	tests := []struct {
		name       string
		responseOp message.Op
		timeoutMsg message.InboundMessage
		inboundMsg message.InboundMessage
	}{
		{
			name:       "CrossChainAppRequest - chain response",
			responseOp: message.CrossChainAppResponseOp,
			timeoutMsg: message.InternalCrossChainAppError(ids.EmptyNodeID, ids.Empty, ids.Empty, wantRequestID, errFoo.Code, errFoo.Message),
			inboundMsg: message.InternalCrossChainAppResponse(ids.EmptyNodeID, ids.Empty, ids.Empty, wantRequestID, wantResponse),
		},
		{
			name:       "CrossChainAppRequest - chain error",
			responseOp: message.CrossChainAppResponseOp,
			timeoutMsg: message.InternalCrossChainAppError(ids.EmptyNodeID, ids.Empty, ids.Empty, wantRequestID, errFoo.Code, errFoo.Message),
			inboundMsg: message.InternalCrossChainAppError(ids.EmptyNodeID, ids.Empty, ids.Empty, wantRequestID, errFoo.Code, errFoo.Message),
		},
		{
			name:       "CrossChainAppRequest - timeout",
			responseOp: message.CrossChainAppResponseOp,
			timeoutMsg: message.InternalCrossChainAppError(ids.EmptyNodeID, ids.Empty, ids.Empty, wantRequestID, errFoo.Code, errFoo.Message),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			wg := &sync.WaitGroup{}
			chainRouter, engine := newChainRouterTest(t)

			wg.Add(1)
			if tt.inboundMsg == nil || tt.inboundMsg.Op() == message.CrossChainAppErrorOp {
				engine.CrossChainAppRequestFailedF = func(_ context.Context, chainID ids.ID, requestID uint32, appErr *common.AppError) error {
					defer wg.Done()
					require.Zero(chainRouter.timedRequests.Len())

					require.Equal(ids.Empty, chainID)
					require.Equal(wantRequestID, requestID)
					require.Equal(errFoo.Code, appErr.Code)
					require.Equal(errFoo.Message, appErr.Message)

					return nil
				}
			} else if tt.inboundMsg.Op() == message.CrossChainAppResponseOp {
				engine.CrossChainAppResponseF = func(_ context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
					defer wg.Done()
					require.Zero(chainRouter.timedRequests.Len())

					require.Equal(ids.Empty, chainID)
					require.Equal(wantRequestID, requestID)
					require.Equal(wantResponse, msg)

					return nil
				}
			}

			ctx := context.Background()
			chainRouter.RegisterRequest(ctx, ids.EmptyNodeID, ids.Empty, ids.Empty, wantRequestID, tt.responseOp, tt.timeoutMsg, engineType)
			require.Equal(1, chainRouter.timedRequests.Len())

			if tt.inboundMsg != nil {
				chainRouter.HandleInbound(ctx, tt.inboundMsg)
			}

			wg.Wait()
		})
	}
}

func newChainRouterTest(t *testing.T) (*ChainRouter, *common.EngineTest) {
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
		"",
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
		"",
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
	h, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
	)
	require.NoError(t, err)

	bootstrapper := &common.BootstrapperTest{
		EngineTest: common.EngineTest{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	h.SetEngineManager(&handler.EngineManager{
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})
	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	h.Start(context.Background(), false)

	t.Cleanup(func() {
		tm.Stop()
		chainRouter.Shutdown(context.Background())
	})

	return chainRouter, engine
}
