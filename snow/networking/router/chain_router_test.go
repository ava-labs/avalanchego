// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
)

func TestShutdown(t *testing.T) {
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestNodeID(), 1)
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
	mc := message.NewInternalBuilder()
	err = chainRouter.Initialize(ids.EmptyNodeID, logging.NoLog{}, mc, tm, time.Second, ids.Set{}, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	require.NoError(t, err)

	shutdownCalled := make(chan struct{}, 1)

	ctx := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(t, err)
	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
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
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.ShutdownF = func() error { shutdownCalled <- struct{}{}; return nil }
	bootstrapper.ConnectedF = func(nodeID ids.NodeID, nodeVersion *version.Application) error { return nil }
	bootstrapper.HaltF = func() {}
	handler.SetBootstrapper(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(true)
	engine.CantGossip = false
	engine.ContextF = func() *snow.ConsensusContext { return ctx }
	engine.ShutdownF = func() error { shutdownCalled <- struct{}{}; return nil }
	engine.ConnectedF = func(nodeID ids.NodeID, nodeVersion *version.Application) error { return nil }
	engine.HaltF = func() {}
	handler.SetConsensus(engine)
	ctx.SetState(snow.NormalOp) // assumed bootstrap is done

	chainRouter.AddChain(handler)

	bootstrapper.StartF = func(startReqID uint32) error { return nil }
	handler.Start(false)

	chainRouter.Shutdown()

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
	err := vdrs.AddWeight(ids.GenerateTestNodeID(), 1)
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

	mc, err := message.NewCreator(metrics, "dummyNamespace", true, 10*time.Second)
	require.NoError(t, err)

	err = chainRouter.Initialize(ids.EmptyNodeID,
		logging.NoLog{},
		mc,
		tm,
		time.Millisecond,
		ids.Set{},
		ids.Set{},
		nil,
		HealthConfig{},
		"",
		metrics,
	)
	require.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(t, err)
	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
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
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.ConnectedF = func(nodeID ids.NodeID, nodeVersion *version.Application) error { return nil }
	bootstrapper.HaltF = func() {}
	bootstrapper.AncestorsF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) error {
		// Ancestors blocks for two seconds
		time.Sleep(2 * time.Second)
		bootstrapFinished <- struct{}{}
		return nil
	}
	handler.SetBootstrapper(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext { return ctx }
	closed := new(int)
	engine.ShutdownF = func() error { *closed++; return nil }
	handler.SetConsensus(engine)
	ctx.SetState(snow.NormalOp) // assumed bootstrapping is done

	chainRouter.AddChain(handler)

	bootstrapper.StartF = func(startReqID uint32) error { return nil }
	handler.Start(false)

	shutdownFinished := make(chan struct{}, 1)

	go func() {
		chainID := ids.ID{}
		msg := mc.InboundAncestors(chainID, 1, nil, nodeID)
		handler.Push(context.Background(), msg)

		time.Sleep(50 * time.Millisecond) // Pause to ensure message gets processed

		chainRouter.Shutdown()
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
	r := require.New(t)
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
	r.NoError(err)
	go tm.Dispatch()

	// Create a router
	chainRouter := ChainRouter{}

	mc := message.NewInternalBuilder()
	err = chainRouter.Initialize(ids.EmptyNodeID, logging.NoLog{}, mc, tm, time.Millisecond, ids.Set{}, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	r.NoError(err)

	// Create bootstrapper, engine and handler
	var (
		calledGetFailed, calledGetAncestorsFailed,
		calledQueryFailed, calledQueryFailed2,
		calledGetAcceptedFailed, calledGetAcceptedFrontierFailed,
		calledCrossChainAppRequestFailed bool

		wg = sync.WaitGroup{}
	)

	ctx := snow.DefaultConsensusContextTest()
	vdrs := validators.NewSet()
	err = vdrs.AddWeight(ids.GenerateTestNodeID(), 1)
	r.NoError(err)

	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	r.NoError(err)
	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
	)
	r.NoError(err)

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
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.ConnectedF = func(nodeID ids.NodeID, nodeVersion *version.Application) error { return nil }
	bootstrapper.HaltF = func() {}
	bootstrapper.GetFailedF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
		wg.Done()
		calledGetFailed = true
		return nil
	}
	bootstrapper.GetAncestorsFailedF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
		defer wg.Done()
		calledGetAncestorsFailed = true
		return nil
	}
	bootstrapper.QueryFailedF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
		defer wg.Done()
		if !calledQueryFailed {
			calledQueryFailed = true
			return nil
		}
		calledQueryFailed2 = true
		return nil
	}
	bootstrapper.GetAcceptedFailedF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
		defer wg.Done()
		calledGetAcceptedFailed = true
		return nil
	}
	bootstrapper.GetAcceptedFrontierFailedF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
		defer wg.Done()
		calledGetAcceptedFrontierFailed = true
		return nil
	}
	bootstrapper.CrossChainAppRequestFailedF = func(ctx context.Context, chainID ids.ID, requestID uint32) error {
		defer wg.Done()
		calledCrossChainAppRequestFailed = true
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.SetState(snow.Bootstrapping) // assumed bootstrapping is ongoing

	chainRouter.AddChain(handler)

	bootstrapper.StartF = func(startReqID uint32) error { return nil }
	handler.Start(false)

	// Register requests for each request type
	msgs := []message.Op{
		message.Put,
		message.Ancestors,
		message.Chits,
		message.Chits,
		message.Accepted,
		message.AcceptedFrontier,
		message.CrossChainAppResponse,
	}

	wg.Add(len(msgs))

	for i, msg := range msgs {
		chainRouter.RegisterRequest(context.Background(), ids.GenerateTestNodeID(), ctx.ChainID, ctx.ChainID, uint32(i), msg)
	}

	wg.Wait()

	chainRouter.lock.Lock()
	defer chainRouter.lock.Unlock()

	r.True(calledGetFailed)
	r.True(calledGetAncestorsFailed)
	r.True(calledQueryFailed2)
	r.True(calledGetAcceptedFailed)
	r.True(calledGetAcceptedFrontierFailed)
	r.True(calledCrossChainAppRequestFailed)
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

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, "dummyNamespace", true, 10*time.Second)
	require.NoError(t, err)

	err = chainRouter.Initialize(ids.EmptyNodeID, logging.NoLog{}, mc, tm, time.Millisecond, ids.Set{}, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	require.NoError(t, err)

	// Create bootstrapper, engine and handler
	ctx := snow.DefaultConsensusContextTest()
	vdrs := validators.NewSet()
	err = vdrs.AddWeight(ids.GenerateTestNodeID(), 1)
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(t, err)
	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
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
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	handler.SetBootstrapper(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext { return ctx }
	handler.SetConsensus(engine)
	ctx.SetState(snow.NormalOp) // assumed bootstrapping is done

	chainRouter.AddChain(handler)

	bootstrapper.StartF = func(startReqID uint32) error { return nil }
	handler.Start(false)

	// Register requests for each request type
	ops := []message.Op{
		message.Put,
		message.Ancestors,
		message.Chits,
		message.Accepted,
		message.AcceptedFrontier,
	}

	vID := ids.GenerateTestNodeID()
	for i, op := range ops {
		chainRouter.RegisterRequest(context.Background(), vID, ctx.ChainID, ctx.ChainID, uint32(i), op)
	}

	// Clear each timeout by simulating responses to the queries
	// Note: Depends on the ordering of [msgs]
	var inMsg message.InboundMessage

	// Put
	inMsg = mc.InboundPut(ctx.ChainID, 0, nil, vID)
	chainRouter.HandleInbound(context.Background(), inMsg)

	// Ancestors
	inMsg = mc.InboundAncestors(ctx.ChainID, 1, nil, vID)
	chainRouter.HandleInbound(context.Background(), inMsg)

	// Chits
	inMsg = mc.InboundChits(ctx.ChainID, 2, nil, vID)
	chainRouter.HandleInbound(context.Background(), inMsg)

	// Accepted
	inMsg = mc.InboundAccepted(ctx.ChainID, 3, nil, vID)
	chainRouter.HandleInbound(context.Background(), inMsg)

	// Accepted Frontier
	inMsg = mc.InboundAcceptedFrontier(ctx.ChainID, 4, nil, vID)
	chainRouter.HandleInbound(context.Background(), inMsg)

	require.Equal(t, chainRouter.timedRequests.Len(), 0)
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

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, "dummyNamespace", true, 10*time.Second)
	require.NoError(t, err)

	err = chainRouter.Initialize(ids.EmptyNodeID, logging.NoLog{}, mc, tm, time.Millisecond, ids.Set{}, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	require.NoError(t, err)

	// Create bootstrapper, engine and handler
	calledF := false
	wg := sync.WaitGroup{}

	ctx := snow.DefaultConsensusContextTest()
	ctx.SetValidatorOnly()
	vdrs := validators.NewSet()
	vID := ids.GenerateTestNodeID()
	err = vdrs.AddWeight(vID, 1)
	require.NoError(t, err)
	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(t, err)
	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
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
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.PullQueryF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
		defer wg.Done()
		calledF = true
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.SetState(snow.Bootstrapping) // assumed bootstrapping is ongoing

	engine := &common.EngineTest{T: t}
	engine.ContextF = func() *snow.ConsensusContext { return ctx }
	engine.Default(false)
	handler.SetConsensus(engine)

	chainRouter.AddChain(handler)

	bootstrapper.StartF = func(startReqID uint32) error { return nil }
	handler.Start(false)

	var inMsg message.InboundMessage
	dummyContainerID := ids.GenerateTestID()
	reqID := uint32(0)

	// Non-validator case
	nID := ids.GenerateTestNodeID()

	calledF = false
	inMsg = mc.InboundPullQuery(ctx.ChainID, reqID, time.Hour, dummyContainerID,
		nID,
	)
	chainRouter.HandleInbound(context.Background(), inMsg)

	require.False(t, calledF) // should not be called

	// Validator case
	calledF = false
	reqID++
	inMsg = mc.InboundPullQuery(ctx.ChainID, reqID, time.Hour, dummyContainerID,
		vID,
	)
	wg.Add(1)
	chainRouter.HandleInbound(context.Background(), inMsg)

	wg.Wait()
	require.True(t, calledF) // should be called since this is a validator request

	// register a validator request
	reqID++
	chainRouter.RegisterRequest(context.Background(), vID, ctx.ChainID, ctx.ChainID, reqID, message.Get)
	require.Equal(t, 1, chainRouter.timedRequests.Len())

	// remove it from validators
	err = vdrs.Set(validators.NewSet().List())
	require.NoError(t, err)

	inMsg = mc.InboundPut(ctx.ChainID, reqID, nil, nID)
	chainRouter.HandleInbound(context.Background(), inMsg)

	// shouldn't clear out timed request, as the request should be cleared when
	// the GetFailed message is sent
	require.Equal(t, 1, chainRouter.timedRequests.Len())
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
	mc := message.NewInternalBuilder()
	chainRouter := ChainRouter{}
	require.NoError(t, chainRouter.Initialize(nodeID, logging.NoLog{}, mc, tm, time.Millisecond, ids.Set{}, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry()))

	// Set up validators
	vdrs := validators.NewSet()
	require.NoError(t, vdrs.AddWeight(ids.GenerateTestNodeID(), 1))

	// Create bootstrapper, engine and handler
	requester := snow.DefaultConsensusContextTest()
	requester.ChainID = ids.GenerateTestID()
	requester.Registerer = prometheus.NewRegistry()
	requester.Metrics = metrics.NewOptionalGatherer()
	requester.Executing(false)

	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(t, err)

	requesterHandler, err := handler.New(
		mc,
		requester,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
	)
	require.NoError(t, err)

	responder := snow.DefaultConsensusContextTest()
	responder.ChainID = ids.GenerateTestID()
	responder.Registerer = prometheus.NewRegistry()
	responder.Metrics = metrics.NewOptionalGatherer()
	responder.Executing(false)

	responderHandler, err := handler.New(
		mc,
		responder,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
	)
	require.NoError(t, err)

	// assumed bootstrapping is done
	responder.SetState(snow.NormalOp)
	requester.SetState(snow.NormalOp)

	// router tracks two chains - one will send a message to the other
	chainRouter.AddChain(requesterHandler)
	chainRouter.AddChain(responderHandler)

	// Each chain should start off with a connected message
	require.Equal(t, 1, chainRouter.chains[requester.ChainID].Len())
	require.Equal(t, 1, chainRouter.chains[responder.ChainID].Len())

	// Requester sends a request to the responder
	msg := []byte("foobar")
	chainRouter.HandleInbound(context.Background(), mc.InternalCrossChainAppRequest(requester.NodeID, requester.ChainID, responder.ChainID, uint32(1), time.Minute, msg))
	require.Equal(t, 2, chainRouter.chains[responder.ChainID].Len())

	// We register the cross-chain response on the requester-side so we don't
	// drop it.
	chainRouter.RegisterRequest(context.Background(), nodeID, requester.ChainID, responder.ChainID, uint32(1), message.CrossChainAppResponse)
	// Responder sends a response back to the requester.
	chainRouter.HandleInbound(context.Background(), mc.InternalCrossChainAppResponse(nodeID, responder.ChainID, requester.ChainID, uint32(1), msg))
	require.Equal(t, 2, chainRouter.chains[requester.ChainID].Len())
}
