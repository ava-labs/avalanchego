// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
)

func TestShutdown(t *testing.T) {
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	benchlist := benchlist.NewNoBenchlist()
	tm := timeout.Manager{}
	err = tm.Initialize(
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
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	chainRouter := ChainRouter{}
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)

	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Second, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	shutdownCalled := make(chan struct{}, 1)

	ctx := snow.DefaultConsensusContextTest()
	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
	)
	assert.NoError(t, err)

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
	bootstrapper.ConnectedF = func(nodeID ids.ShortID, nodeVersion version.Application) error { return nil }
	bootstrapper.HaltF = func() {}
	handler.SetBootstrapper(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(true)
	engine.CantGossip = false
	engine.ContextF = func() *snow.ConsensusContext { return ctx }
	engine.ShutdownF = func() error { shutdownCalled <- struct{}{}; return nil }
	engine.ConnectedF = func(nodeID ids.ShortID, nodeVersion version.Application) error { return nil }
	engine.HaltF = func() {}
	handler.SetConsensus(engine)
	ctx.SetState(snow.NormalOp) // assumed bootstrap is done

	chainRouter.AddChain(handler)
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
	nodeID := ids.ShortEmpty
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	benchlist := benchlist.NewNoBenchlist()
	tm := timeout.Manager{}
	metrics := prometheus.NewRegistry()
	// Ensure that the Ancestors request does not timeout
	err = tm.Initialize(
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
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	chainRouter := ChainRouter{}

	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)

	err = chainRouter.Initialize(ids.ShortEmpty,
		logging.NoLog{},
		mc,
		&tm,
		time.Millisecond,
		ids.Set{},
		nil,
		HealthConfig{},
		"",
		metrics,
	)
	assert.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()
	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
	)
	assert.NoError(t, err)

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
	bootstrapper.ConnectedF = func(nodeID ids.ShortID, nodeVersion version.Application) error { return nil }
	bootstrapper.HaltF = func() {}
	bootstrapper.AncestorsF = func(nodeID ids.ShortID, requestID uint32, containers [][]byte) error {
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
	handler.Start(false)

	shutdownFinished := make(chan struct{}, 1)

	go func() {
		chainID := ids.ID{}
		msg := mc.InboundAncestors(chainID, 1, nil, nodeID)
		handler.Push(msg)

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
	// Create a timeout manager
	maxTimeout := 25 * time.Millisecond
	tm := timeout.Manager{}
	err := tm.Initialize(
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
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	// Create a router
	chainRouter := ChainRouter{}
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)

	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	// Create bootstrapper, engine and handler
	var (
		calledGetFailed, calledGetAncestorsFailed,
		calledQueryFailed, calledQueryFailed2,
		calledGetAcceptedFailed, calledGetAcceptedFrontierFailed bool

		wg = sync.WaitGroup{}
	)

	ctx := snow.DefaultConsensusContextTest()
	vdrs := validators.NewSet()
	err = vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)

	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
	)
	assert.NoError(t, err)

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
	bootstrapper.ConnectedF = func(nodeID ids.ShortID, nodeVersion version.Application) error { return nil }
	bootstrapper.HaltF = func() {}
	bootstrapper.GetFailedF = func(nodeID ids.ShortID, requestID uint32) error { wg.Done(); calledGetFailed = true; return nil }
	bootstrapper.GetAncestorsFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		defer wg.Done()
		calledGetAncestorsFailed = true
		return nil
	}
	bootstrapper.QueryFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		defer wg.Done()
		if !calledQueryFailed {
			calledQueryFailed = true
			return nil
		}
		calledQueryFailed2 = true
		return nil
	}
	bootstrapper.GetAcceptedFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		defer wg.Done()
		calledGetAcceptedFailed = true
		return nil
	}
	bootstrapper.GetAcceptedFrontierFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		defer wg.Done()
		calledGetAcceptedFrontierFailed = true
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.SetState(snow.Bootstrapping) // assumed bootstrapping is ongoing

	chainRouter.AddChain(handler)
	handler.Start(false)

	// Register requests for each request type
	msgs := []message.Op{
		message.Put,
		message.Ancestors,
		message.Chits,
		message.Chits,
		message.Accepted,
		message.AcceptedFrontier,
	}

	wg.Add(len(msgs))

	for i, msg := range msgs {
		chainRouter.RegisterRequest(ids.GenerateTestShortID(), ctx.ChainID, uint32(i), msg)
	}

	wg.Wait()

	chainRouter.lock.Lock()
	defer chainRouter.lock.Unlock()
	assert.True(t, calledGetFailed && calledGetAncestorsFailed && calledQueryFailed2 && calledGetAcceptedFailed && calledGetAcceptedFrontierFailed)
}

func TestRouterClearTimeouts(t *testing.T) {
	// Create a timeout manager
	tm := timeout.Manager{}
	err := tm.Initialize(
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
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	// Create a router
	chainRouter := ChainRouter{}
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)

	assert.NoError(t, err)
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	// Create bootstrapper, engine and handler
	ctx := snow.DefaultConsensusContextTest()
	vdrs := validators.NewSet()
	err = vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)

	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
	)
	assert.NoError(t, err)

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
	handler.Start(false)

	// Register requests for each request type
	ops := []message.Op{
		message.Put,
		message.Ancestors,
		message.Chits,
		message.Accepted,
		message.AcceptedFrontier,
	}

	vID := ids.GenerateTestShortID()
	for i, op := range ops {
		chainRouter.RegisterRequest(vID, ctx.ChainID, uint32(i), op)
	}

	// Clear each timeout by simulating responses to the queries
	// Note: Depends on the ordering of [msgs]
	var inMsg message.InboundMessage

	// Put
	inMsg = mc.InboundPut(ctx.ChainID, 0, ids.GenerateTestID(), nil, vID)
	chainRouter.HandleInbound(inMsg)

	// Ancestors
	inMsg = mc.InboundAncestors(ctx.ChainID, 1, nil, vID)
	chainRouter.HandleInbound(inMsg)

	// Chits
	inMsg = mc.InboundChits(ctx.ChainID, 2, nil, vID)
	chainRouter.HandleInbound(inMsg)

	// Accepted
	inMsg = mc.InboundAccepted(ctx.ChainID, 3, nil, vID)
	chainRouter.HandleInbound(inMsg)

	// Accepted Frontier
	inMsg = mc.InboundAcceptedFrontier(ctx.ChainID, 4, nil, vID)
	chainRouter.HandleInbound(inMsg)

	assert.Equal(t, chainRouter.timedRequests.Len(), 0)
}

func TestValidatorOnlyMessageDrops(t *testing.T) {
	// Create a timeout manager
	maxTimeout := 25 * time.Millisecond
	tm := timeout.Manager{}
	err := tm.Initialize(
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
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	// Create a router
	chainRouter := ChainRouter{}
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)

	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	// Create bootstrapper, engine and handler
	calledF := false
	wg := sync.WaitGroup{}

	ctx := snow.DefaultConsensusContextTest()
	ctx.SetValidatorOnly()
	vdrs := validators.NewSet()
	vID := ids.GenerateTestShortID()
	err = vdrs.AddWeight(vID, 1)
	assert.NoError(t, err)

	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
	)
	assert.NoError(t, err)

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
	bootstrapper.PullQueryF = func(nodeID ids.ShortID, requestID uint32, containerID ids.ID) error {
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
	handler.Start(false)

	var inMsg message.InboundMessage
	dummyContainerID := ids.GenerateTestID()
	reqID := uint32(0)

	// Non-validator case
	nID := ids.GenerateTestShortID()

	calledF = false
	inMsg = mc.InboundPullQuery(ctx.ChainID, reqID, time.Hour, dummyContainerID,
		nID,
	)
	chainRouter.HandleInbound(inMsg)

	assert.False(t, calledF) // should not be called

	// Validator case
	calledF = false
	reqID++
	inMsg = mc.InboundPullQuery(ctx.ChainID, reqID, time.Hour, dummyContainerID,
		vID,
	)
	wg.Add(1)
	chainRouter.HandleInbound(inMsg)

	wg.Wait()
	assert.True(t, calledF) // should be called since this is a validator request

	// register a validator request
	reqID++
	chainRouter.RegisterRequest(vID, ctx.ChainID, reqID, message.Get)
	assert.Equal(t, 1, chainRouter.timedRequests.Len())

	// remove it from validators
	err = vdrs.Set(validators.NewSet().List())
	assert.NoError(t, err)

	inMsg = mc.InboundPut(ctx.ChainID, reqID, ids.GenerateTestID(), nil, nID)
	chainRouter.HandleInbound(inMsg)

	// shouldn't clear out timed request, as the request should be cleared when
	// the GetFailed message is sent
	assert.Equal(t, 1, chainRouter.timedRequests.Len())
}
