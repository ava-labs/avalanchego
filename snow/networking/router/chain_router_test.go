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
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
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

	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Hour, time.Second, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	engine := common.EngineTest{T: t}
	engine.Default(false)

	shutdownCalled := make(chan struct{}, 1)

	engine.ContextF = snow.DefaultConsensusContextTest
	engine.ShutdownF = func() error { shutdownCalled <- struct{}{}; return nil }

	handler := &Handler{}
	err = handler.Initialize(
		mc,
		&engine,
		vdrs,
		nil,
	)
	assert.NoError(t, err)

	go handler.Dispatch()

	chainRouter.AddChain(handler)

	chainRouter.Shutdown()

	ticker := time.NewTicker(250 * time.Millisecond)
	select {
	case <-ticker.C:
		t.Fatalf("Handler shutdown was not called or timed out after 250ms during chainRouter shutdown")
	case <-shutdownCalled:
	}

	select {
	case <-handler.closed:
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
	// Ensure that the MultiPut request does not timeout
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
		time.Hour,
		time.Millisecond,
		ids.Set{},
		nil,
		HealthConfig{},
		"",
		metrics,
	)
	assert.NoError(t, err)

	engine := common.EngineTest{T: t}
	engine.Default(false)

	engineFinished := make(chan struct{}, 1)

	// MultiPut blocks for two seconds
	engine.MultiPutF = func(nodeID ids.ShortID, requestID uint32, containers [][]byte) error {
		time.Sleep(2 * time.Second)
		engineFinished <- struct{}{}
		return nil
	}

	closed := new(int)

	engine.ContextF = snow.DefaultConsensusContextTest
	engine.ShutdownF = func() error { *closed++; return nil }

	handler := &Handler{}
	err = handler.Initialize(
		mc,
		&engine,
		vdrs,
		nil,
	)
	assert.NoError(t, err)

	chainRouter.AddChain(handler)

	go handler.Dispatch()

	shutdownFinished := make(chan struct{}, 1)

	go func() {
		chainID := ids.ID{}
		msg := mc.InboundMultiPut(chainID, 1, nil, nodeID)
		handler.Push(msg)

		time.Sleep(50 * time.Millisecond) // Pause to ensure message gets processed

		chainRouter.Shutdown()
		shutdownFinished <- struct{}{}
	}()

	select {
	case <-engineFinished:
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

	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Hour, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	// Create an engine and handler
	engine := common.EngineTest{T: t}
	engine.Default(false)

	var (
		calledGetFailed, calledGetAncestorsFailed,
		calledQueryFailed, calledQueryFailed2,
		calledGetAcceptedFailed, calledGetAcceptedFrontierFailed bool

		wg = sync.WaitGroup{}
	)

	engine.GetFailedF = func(nodeID ids.ShortID, requestID uint32) error { wg.Done(); calledGetFailed = true; return nil }
	engine.GetAncestorsFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		defer wg.Done()
		calledGetAncestorsFailed = true
		return nil
	}
	engine.QueryFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		defer wg.Done()
		if !calledQueryFailed {
			calledQueryFailed = true
			return nil
		}
		calledQueryFailed2 = true
		return nil
	}
	engine.GetAcceptedFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		defer wg.Done()
		calledGetAcceptedFailed = true
		return nil
	}
	engine.GetAcceptedFrontierFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		defer wg.Done()
		calledGetAcceptedFrontierFailed = true
		return nil
	}

	engine.ContextF = snow.DefaultConsensusContextTest

	handler := &Handler{}
	vdrs := validators.NewSet()
	err = vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	err = handler.Initialize(
		mc,
		&engine,
		vdrs,
		nil,
	)
	assert.NoError(t, err)

	chainRouter.AddChain(handler)
	go handler.Dispatch()

	// Register requests for each request type
	msgs := []message.Op{
		message.Put,
		message.MultiPut,
		message.Chits,
		message.Chits,
		message.Accepted,
		message.AcceptedFrontier,
	}

	wg.Add(len(msgs))

	for i, msg := range msgs {
		chainRouter.RegisterRequest(ids.GenerateTestShortID(), handler.ctx.ChainID, uint32(i), msg)
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
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Hour, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	// Create an engine and handler
	engine := common.EngineTest{T: t}
	engine.Default(false)

	engine.ContextF = snow.DefaultConsensusContextTest

	vdrs := validators.NewSet()
	err = vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	handler := &Handler{}
	err = handler.Initialize(
		mc,
		&engine,
		vdrs,
		nil,
	)
	assert.NoError(t, err)

	chainRouter.AddChain(handler)
	go handler.Dispatch()

	// Register requests for each request type
	ops := []message.Op{
		message.Put,
		message.MultiPut,
		message.Chits,
		message.Accepted,
		message.AcceptedFrontier,
	}

	vID := ids.GenerateTestShortID()
	for i, op := range ops {
		chainRouter.RegisterRequest(vID, handler.ctx.ChainID, uint32(i), op)
	}

	// Clear each timeout by simulating responses to the queries
	// Note: Depends on the ordering of [msgs]
	var inMsg message.InboundMessage

	// Put
	inMsg = mc.InboundPut(handler.ctx.ChainID, 0, ids.GenerateTestID(), nil, vID)
	chainRouter.HandleInbound(inMsg)

	// MultiPut
	inMsg = mc.InboundMultiPut(handler.ctx.ChainID, 1, nil, vID)
	chainRouter.HandleInbound(inMsg)

	// Chits
	inMsg = mc.InboundChits(handler.ctx.ChainID, 2, nil, vID)
	chainRouter.HandleInbound(inMsg)

	// Accepted
	inMsg = mc.InboundAccepted(handler.ctx.ChainID, 3, nil, vID)
	chainRouter.HandleInbound(inMsg)

	// Accepted Frontier
	inMsg = mc.InboundAcceptedFrontier(handler.ctx.ChainID, 4, nil, vID)
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

	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Hour, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	// Create an engine and handler
	engine := common.EngineTest{T: t}
	engine.Default(false)

	calledF := new(bool)

	wg := sync.WaitGroup{}

	engine.PullQueryF = func(nodeID ids.ShortID, requestID uint32, containerID ids.ID) error {
		defer wg.Done()
		*calledF = true
		return nil
	}

	engine.ContextF = func() *snow.ConsensusContext {
		ctx := snow.DefaultConsensusContextTest()
		ctx.SetValidatorOnly()
		return ctx
	}

	handler := &Handler{}
	vdrs := validators.NewSet()
	vID := ids.GenerateTestShortID()
	err = vdrs.AddWeight(vID, 1)
	assert.NoError(t, err)
	err = handler.Initialize(
		mc,
		&engine,
		vdrs,
		nil,
	)
	assert.NoError(t, err)

	chainRouter.AddChain(handler)
	go handler.Dispatch()

	// generate a non-validator ID
	nID := ids.GenerateTestShortID()

	*calledF = false
	var inMsg message.InboundMessage

	inMsg = mc.InboundPullQuery(handler.ctx.ChainID, uint32(1),
		time.Hour,
		ids.GenerateTestID(),
		nID,
	)
	chainRouter.HandleInbound(inMsg)
	assert.False(t, *calledF) // should not be called

	// validator case
	*calledF = false
	wg.Add(1)
	inMsg = mc.InboundPullQuery(handler.ctx.ChainID,
		uint32(2),
		time.Hour,
		ids.GenerateTestID(),
		vID,
	)
	chainRouter.HandleInbound(inMsg)

	wg.Wait()
	// should be called since this is a validator request
	assert.True(t, *calledF)

	// register a validator request
	reqID := uint32(3)
	chainRouter.RegisterRequest(vID, handler.ctx.ChainID, reqID, message.Get)
	assert.Equal(t, 1, chainRouter.timedRequests.Len())

	// remove it from validators
	err = handler.validators.Set(validators.NewSet().List())
	assert.NoError(t, err)

	inMsg = mc.InboundPut(handler.ctx.ChainID, reqID, ids.GenerateTestID(), nil, nID)
	chainRouter.HandleInbound(inMsg)

	// shouldn't clear out timed request, as the request should be cleared when
	// the GetFailed message is sent
	assert.Equal(t, 1, chainRouter.timedRequests.Len())
}
