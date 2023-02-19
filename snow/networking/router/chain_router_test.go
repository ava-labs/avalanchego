// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
)

const engineType = p2p.EngineType_ENGINE_TYPE_AVALANCHE

func TestShutdown(t *testing.T) {
	vdrs := validators.NewSet()
	err := vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
	require.NoError(t, err)
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
	require.NoError(t, err)
	go tm.Dispatch()

	chainRouter := ChainRouter{}
	err = chainRouter.Initialize(
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
	)
	require.NoError(t, err)

	shutdownCalled := make(chan struct{}, 1)

	ctx := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handler, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
	)
	require.NoError(t, err)

	bootstrapper := &common.BootstrapperTest{
		BootstrapableTest: common.BootstrapableTest{
			T: t,
		},
		EngineTest: common.EngineTest{
			T: t,
		},
	}
	bootstrapper.Default(true)
	bootstrapper.CantGossip = false
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.ShutdownF = func(context.Context) error {
		shutdownCalled <- struct{}{}
		return nil
	}
	bootstrapper.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}
	bootstrapper.HaltF = func(context.Context) {}
	handler.SetBootstrapper(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(true)
	engine.CantGossip = false
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	engine.ShutdownF = func(context.Context) error {
		shutdownCalled <- struct{}{}
		return nil
	}
	engine.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}
	engine.HaltF = func(context.Context) {}
	handler.SetConsensus(engine)
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(context.Background(), handler)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	handler.Start(context.Background(), false)

	chainRouter.Shutdown(context.Background())

	ticker := time.NewTicker(250 * time.Millisecond)
	select {
	case <-ticker.C:
		t.Fatalf("Handler shutdown was not called or timed out after 250ms during chainRouter shutdown")
	case <-shutdownCalled:
	}

	select {
	case <-handler.Stopped():
	default:
		t.Fatal("handler shutdown but never closed its closing channel")
	}
}

func TestShutdownTimesOut(t *testing.T) {
	nodeID := ids.EmptyNodeID
	vdrs := validators.NewSet()
	err := vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
	require.NoError(t, err)
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
	require.NoError(t, err)
	go tm.Dispatch()

	chainRouter := ChainRouter{}

	err = chainRouter.Initialize(
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
	)
	require.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handler, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
	)
	require.NoError(t, err)

	bootstrapFinished := make(chan struct{}, 1)
	bootstrapper := &common.BootstrapperTest{
		BootstrapableTest: common.BootstrapableTest{
			T: t,
		},
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
	bootstrapper.PullQueryF = func(context.Context, ids.NodeID, uint32, ids.ID) error {
		// Ancestors blocks for two seconds
		time.Sleep(2 * time.Second)
		bootstrapFinished <- struct{}{}
		return nil
	}
	handler.SetBootstrapper(bootstrapper)

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
	handler.SetConsensus(engine)
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(context.Background(), handler)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	handler.Start(context.Background(), false)

	shutdownFinished := make(chan struct{}, 1)

	go func() {
		chainID := ids.ID{}
		msg := message.InboundPullQuery(chainID, 1, time.Hour, ids.GenerateTestID(), nodeID, engineType)
		handler.Push(context.Background(), msg)

		time.Sleep(50 * time.Millisecond) // Pause to ensure message gets processed

		chainRouter.Shutdown(context.Background())
		shutdownFinished <- struct{}{}
	}()

	select {
	case <-bootstrapFinished:
		t.Fatalf("Shutdown should have finished in one millisecond before timing out instead of waiting for engine to finish shutting down.")
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

	// Create a router
	chainRouter := ChainRouter{}

	err = chainRouter.Initialize(
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
	)
	require.NoError(err)

	// Create bootstrapper, engine and handler
	var (
		calledGetStateSummaryFrontierFailed, calledGetAcceptedStateSummaryFailed,
		calledGetAcceptedFrontierFailed, calledGetAcceptedFailed,
		calledGetAncestorsFailed,
		calledGetFailed, calledQueryFailed,
		calledAppRequestFailed,
		calledCrossChainAppRequestFailed bool

		wg = sync.WaitGroup{}
	)

	ctx := snow.DefaultConsensusContextTest()
	vdrs := validators.NewSet()
	err = vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
	require.NoError(err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	handler, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
	)
	require.NoError(err)

	bootstrapper := &common.BootstrapperTest{
		BootstrapableTest: common.BootstrapableTest{
			T: t,
		},
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
	bootstrapper.AppRequestFailedF = func(context.Context, ids.NodeID, uint32) error {
		defer wg.Done()
		calledAppRequestFailed = true
		return nil
	}
	bootstrapper.CrossChainAppRequestFailedF = func(context.Context, ids.ID, uint32) error {
		defer wg.Done()
		calledCrossChainAppRequestFailed = true
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.Bootstrapping, // assumed bootstrapping is ongoing
	})

	chainRouter.AddChain(context.Background(), handler)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	handler.Start(context.Background(), false)

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
				engineType,
			),
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
				engineType,
			),
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
				engineType,
			),
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
				engineType,
			),
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
				engineType,
			),
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
			message.InternalAppRequestFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
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
			message.InternalCrossChainAppRequestFailed(
				nodeID,
				ctx.ChainID,
				ctx.ChainID,
				requestID,
			),
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

func TestRouterClearTimeouts(t *testing.T) {
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
	chainRouter := ChainRouter{}
	err = chainRouter.Initialize(
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
	)
	require.NoError(t, err)

	// Create bootstrapper, engine and handler
	ctx := snow.DefaultConsensusContextTest()
	vdrs := validators.NewSet()
	err = vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handler, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
	)
	require.NoError(t, err)

	bootstrapper := &common.BootstrapperTest{
		BootstrapableTest: common.BootstrapableTest{
			T: t,
		},
		EngineTest: common.EngineTest{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	handler.SetBootstrapper(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	handler.SetConsensus(engine)
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp, // assumed bootstrapping is done
	})

	chainRouter.AddChain(context.Background(), handler)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	handler.Start(context.Background(), false)

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
		)
		msg := message.InboundStateSummaryFrontier(
			ctx.ChainID,
			requestID,
			nil,
			nodeID,
		)
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
		)
		msg := message.InboundAcceptedStateSummary(
			ctx.ChainID,
			requestID,
			nil,
			nodeID,
		)
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
			message.AcceptedFrontierOp,
			message.InternalGetAcceptedFrontierFailed(
				nodeID,
				ctx.ChainID,
				requestID,
				engineType,
			),
		)
		msg := message.InboundAcceptedFrontier(
			ctx.ChainID,
			requestID,
			nil,
			nodeID,
			engineType,
		)
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
			message.AcceptedOp,
			message.InternalGetAcceptedFailed(
				nodeID,
				ctx.ChainID,
				requestID,
				engineType,
			),
		)
		msg := message.InboundAccepted(
			ctx.ChainID,
			requestID,
			nil,
			nodeID,
			engineType,
		)
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
			message.ChitsOp,
			message.InternalQueryFailed(
				nodeID,
				ctx.ChainID,
				requestID,
				engineType,
			),
		)
		msg := message.InboundChits(
			ctx.ChainID,
			requestID,
			nil,
			nil,
			nodeID,
			engineType,
		)
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
			message.AppResponseOp,
			message.InternalAppRequestFailed(
				nodeID,
				ctx.ChainID,
				requestID,
			),
		)
		msg := message.InboundAppResponse(
			ctx.ChainID,
			requestID,
			nil,
			nodeID,
		)
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
			message.CrossChainAppResponseOp,
			message.InternalCrossChainAppRequestFailed(
				nodeID,
				ctx.ChainID,
				ctx.ChainID,
				requestID,
			),
		)
		msg := message.InternalCrossChainAppResponse(
			nodeID,
			ctx.ChainID,
			ctx.ChainID,
			requestID,
			nil,
		)
		chainRouter.HandleInbound(context.Background(), msg)
	}

	require.Equal(t, 0, chainRouter.timedRequests.Len())
}

func TestValidatorOnlyMessageDrops(t *testing.T) {
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
	require.NoError(t, err)
	go tm.Dispatch()

	// Create a router
	chainRouter := ChainRouter{}
	err = chainRouter.Initialize(
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
	)
	require.NoError(t, err)

	// Create bootstrapper, engine and handler
	calledF := false
	wg := sync.WaitGroup{}

	ctx := snow.DefaultConsensusContextTest()
	sb := subnets.New(ctx.NodeID, subnets.Config{ValidatorOnly: true})
	vdrs := validators.NewSet()
	vID := ids.GenerateTestNodeID()
	err = vdrs.Add(vID, nil, ids.Empty, 1)
	require.NoError(t, err)
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handler, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		sb,
	)
	require.NoError(t, err)

	bootstrapper := &common.BootstrapperTest{
		BootstrapableTest: common.BootstrapableTest{
			T: t,
		},
		EngineTest: common.EngineTest{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.PullQueryF = func(context.Context, ids.NodeID, uint32, ids.ID) error {
		defer wg.Done()
		calledF = true
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.Bootstrapping, // assumed bootstrapping is ongoing
	})

	engine := &common.EngineTest{T: t}
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	engine.Default(false)
	handler.SetConsensus(engine)

	chainRouter.AddChain(context.Background(), handler)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	handler.Start(context.Background(), false)

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
		nID,
		engineType,
	)
	chainRouter.HandleInbound(context.Background(), inMsg)

	require.False(t, calledF) // should not be called

	// Validator case
	calledF = false
	reqID++
	inMsg = message.InboundPullQuery(
		ctx.ChainID,
		reqID,
		time.Hour,
		dummyContainerID,
		vID,
		engineType,
	)
	wg.Add(1)
	chainRouter.HandleInbound(context.Background(), inMsg)

	wg.Wait()
	require.True(t, calledF) // should be called since this is a validator request
}

func TestRouterCrossChainMessages(t *testing.T) {
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
	require.NoError(t, err)
	go tm.Dispatch()

	// Create chain router
	nodeID := ids.GenerateTestNodeID()
	chainRouter := ChainRouter{}
	err = chainRouter.Initialize(
		nodeID,
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
	)
	require.NoError(t, err)

	// Set up validators
	vdrs := validators.NewSet()
	require.NoError(t, vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	// Create bootstrapper, engine and handler
	requester := snow.DefaultConsensusContextTest()
	requester.ChainID = ids.GenerateTestID()
	requester.Registerer = prometheus.NewRegistry()
	requester.Metrics = metrics.NewOptionalGatherer()
	requester.Executing.Set(false)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)

	requesterHandler, err := handler.New(
		requester,
		vdrs,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(requester.NodeID, subnets.Config{}),
	)
	require.NoError(t, err)

	responder := snow.DefaultConsensusContextTest()
	responder.ChainID = ids.GenerateTestID()
	responder.Registerer = prometheus.NewRegistry()
	responder.Metrics = metrics.NewOptionalGatherer()
	responder.Executing.Set(false)

	responderHandler, err := handler.New(
		responder,
		vdrs,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(responder.NodeID, subnets.Config{}),
	)
	require.NoError(t, err)

	// assumed bootstrapping is done
	responder.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp,
	})
	requester.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp,
	})

	// router tracks two chains - one will send a message to the other
	chainRouter.AddChain(context.Background(), requesterHandler)
	chainRouter.AddChain(context.Background(), responderHandler)

	// Each chain should start off with a connected message
	require.Equal(t, 1, chainRouter.chainHandlers[requester.ChainID].Len())
	require.Equal(t, 1, chainRouter.chainHandlers[responder.ChainID].Len())

	// Requester sends a request to the responder
	msgBytes := []byte("foobar")
	msg := message.InternalCrossChainAppRequest(
		requester.NodeID,
		requester.ChainID,
		responder.ChainID,
		uint32(1),
		time.Minute,
		msgBytes,
	)
	chainRouter.HandleInbound(context.Background(), msg)
	require.Equal(t, 2, chainRouter.chainHandlers[responder.ChainID].Len())

	// We register the cross-chain response on the requester-side so we don't
	// drop it.
	chainRouter.RegisterRequest(
		context.Background(),
		nodeID,
		requester.ChainID,
		responder.ChainID,
		uint32(1),
		message.CrossChainAppResponseOp,
		message.InternalCrossChainAppRequestFailed(
			nodeID,
			responder.ChainID,
			requester.ChainID,
			uint32(1),
		),
	)
	// Responder sends a response back to the requester.
	msg = message.InternalCrossChainAppResponse(
		nodeID,
		responder.ChainID,
		requester.ChainID,
		uint32(1),
		msgBytes,
	)
	chainRouter.HandleInbound(context.Background(), msg)
	require.Equal(t, 2, chainRouter.chainHandlers[requester.ChainID].Len())
}

func TestConnectedSubnet(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

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
	require.NoError(t, err)
	go tm.Dispatch()

	// Create chain router
	myNodeID := ids.GenerateTestNodeID()
	peerNodeID := ids.GenerateTestNodeID()
	subnetID0 := ids.GenerateTestID()
	subnetID1 := ids.GenerateTestID()
	trackedSubnets := set.Set[ids.ID]{}
	trackedSubnets.Add(subnetID0, subnetID1)
	chainRouter := ChainRouter{}
	err = chainRouter.Initialize(
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
	)
	require.NoError(t, err)

	// Create bootstrapper, engine and handler
	platform := snow.DefaultConsensusContextTest()
	platform.ChainID = constants.PlatformChainID
	platform.SubnetID = constants.PrimaryNetworkID
	platform.Registerer = prometheus.NewRegistry()
	platform.Metrics = metrics.NewOptionalGatherer()
	platform.Executing.Set(false)
	platform.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.NormalOp,
	})

	myConnectedMsg := message.InternalConnected(myNodeID, version.CurrentApp)
	mySubnetConnectedMsg0 := message.InternalConnectedSubnet(myNodeID, subnetID0)
	mySubnetConnectedMsg1 := message.InternalConnectedSubnet(myNodeID, subnetID1)

	platformHandler := handler.NewMockHandler(ctrl)
	platformHandler.EXPECT().Context().Return(platform).AnyTimes()
	platformHandler.EXPECT().SetOnStopped(gomock.Any()).AnyTimes()
	platformHandler.EXPECT().Push(gomock.Any(), myConnectedMsg).Times(1)
	platformHandler.EXPECT().Push(gomock.Any(), mySubnetConnectedMsg0).Times(1)
	platformHandler.EXPECT().Push(gomock.Any(), mySubnetConnectedMsg1).Times(1)

	chainRouter.AddChain(context.Background(), platformHandler)

	peerConnectedMsg := message.InternalConnected(peerNodeID, version.CurrentApp)
	platformHandler.EXPECT().Push(gomock.Any(), peerConnectedMsg).Times(1)
	chainRouter.Connected(peerNodeID, version.CurrentApp, constants.PrimaryNetworkID)

	peerSubnetConnectedMsg0 := message.InternalConnectedSubnet(peerNodeID, subnetID0)
	platformHandler.EXPECT().Push(gomock.Any(), peerSubnetConnectedMsg0).Times(1)
	chainRouter.Connected(peerNodeID, version.CurrentApp, subnetID0)

	myDisconnectedMsg := message.InternalDisconnected(myNodeID)
	platformHandler.EXPECT().Push(gomock.Any(), myDisconnectedMsg).Times(1)
	chainRouter.Benched(constants.PlatformChainID, myNodeID)

	peerDisconnectedMsg := message.InternalDisconnected(peerNodeID)
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
	require.NoError(t, err)
	go tm.Dispatch()

	// Create a router
	chainRouter := ChainRouter{}
	err = chainRouter.Initialize(
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
	)
	require.NoError(t, err)

	// Create bootstrapper, engine and handler
	calledF := false
	wg := sync.WaitGroup{}

	ctx := snow.DefaultConsensusContextTest()
	allowedID := ids.GenerateTestNodeID()
	allowedSet := set.NewSet[ids.NodeID](1)
	allowedSet.Add(allowedID)
	sb := subnets.New(ctx.NodeID, subnets.Config{ValidatorOnly: true, AllowedNodes: allowedSet})

	vdrs := validators.NewSet()
	vID := ids.GenerateTestNodeID()
	err = vdrs.Add(vID, nil, ids.Empty, 1)
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)

	handler, err := handler.New(
		ctx,
		vdrs,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		sb,
	)
	require.NoError(t, err)

	bootstrapper := &common.BootstrapperTest{
		BootstrapableTest: common.BootstrapableTest{
			T: t,
		},
		EngineTest: common.EngineTest{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.PullQueryF = func(context.Context, ids.NodeID, uint32, ids.ID) error {
		defer wg.Done()
		calledF = true
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.State.Set(snow.EngineState{
		Type:  engineType,
		State: snow.Bootstrapping, // assumed bootstrapping is ongoing
	})
	engine := &common.EngineTest{T: t}
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	engine.Default(false)
	handler.SetConsensus(engine)

	chainRouter.AddChain(context.Background(), handler)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	handler.Start(context.Background(), false)

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
		nID,
		engineType,
	)
	chainRouter.HandleInbound(context.Background(), inMsg)

	require.False(t, calledF) // should not be called for unallowed node ID

	// Allowed NodeID case
	calledF = false
	reqID++
	inMsg = message.InboundPullQuery(
		ctx.ChainID,
		reqID,
		time.Hour,
		dummyContainerID,
		allowedID,
		engineType,
	)
	wg.Add(1)
	chainRouter.HandleInbound(context.Background(), inMsg)

	wg.Wait()
	require.True(t, calledF) // should be called since this is a allowed node request

	// Validator case
	calledF = false
	reqID++
	inMsg = message.InboundPullQuery(
		ctx.ChainID,
		reqID,
		time.Hour,
		dummyContainerID,
		vID,
		engineType,
	)
	wg.Add(1)
	chainRouter.HandleInbound(context.Background(), inMsg)

	wg.Wait()
	require.True(t, calledF) // should be called since this is a validator request
}
