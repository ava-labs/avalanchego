// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/constants"
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
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Second, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	engine := common.EngineTest{T: t}
	engine.Default(false)

	shutdownCalled := make(chan struct{}, 1)

	engine.ContextF = snow.DefaultContextTest
	engine.ShutdownF = func() error { shutdownCalled <- struct{}{}; return nil }

	handler := &Handler{}
	err = handler.Initialize(
		&engine,
		vdrs,
		nil,
		"",
		prometheus.NewRegistry(),
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
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	benchlist := benchlist.NewNoBenchlist()
	tm := timeout.Manager{}
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
		prometheus.NewRegistry(),
	)
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	chainRouter := ChainRouter{}
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
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

	engine.ContextF = snow.DefaultContextTest
	engine.ShutdownF = func() error { *closed++; return nil }

	handler := &Handler{}
	err = handler.Initialize(
		&engine,
		vdrs,
		nil,
		"",
		prometheus.NewRegistry(),
	)
	assert.NoError(t, err)

	chainRouter.AddChain(handler)

	go handler.Dispatch()

	shutdownFinished := make(chan struct{}, 1)

	go func() {
		handler.MultiPut(ids.ShortID{}, 1, nil, func() {})
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
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
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

	engine.ContextF = snow.DefaultContextTest

	handler := &Handler{}
	vdrs := validators.NewSet()
	err = vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	err = handler.Initialize(
		&engine,
		vdrs,
		nil,
		"",
		prometheus.NewRegistry(),
	)
	assert.NoError(t, err)

	chainRouter.AddChain(handler)
	go handler.Dispatch()

	// Register requests for each request type
	msgs := []constants.MsgType{
		constants.GetMsg,
		constants.GetAncestorsMsg,
		constants.PullQueryMsg,
		constants.PushQueryMsg,
		constants.GetAcceptedMsg,
		constants.GetAcceptedFrontierMsg,
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
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	// Create an engine and handler
	engine := common.EngineTest{T: t}
	engine.Default(false)

	engine.ContextF = snow.DefaultContextTest

	vdrs := validators.NewSet()
	err = vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	handler := &Handler{}
	err = handler.Initialize(
		&engine,
		vdrs,
		nil,
		"",
		prometheus.NewRegistry(),
	)
	assert.NoError(t, err)

	chainRouter.AddChain(handler)
	go handler.Dispatch()

	// Register requests for each request type
	msgs := []constants.MsgType{
		constants.GetMsg,
		constants.GetAncestorsMsg,
		constants.PullQueryMsg,
		constants.PushQueryMsg,
		constants.GetAcceptedMsg,
		constants.GetAcceptedFrontierMsg,
	}

	// create messages builder
	metrics := prometheus.NewRegistry()
	mc, err := message.NewMsgCreator(metrics, true /*compress*/)
	if err != nil {
		panic(err)
	}

	vID := ids.GenerateTestShortID()
	for i, msg := range msgs {
		chainRouter.RegisterRequest(vID, handler.ctx.ChainID, uint32(i), msg)
	}

	// Clear each timeout by simulating responses to the queries
	// Note: Depends on the ordering of [msgs]
	var inMsg message.InboundMessage

	// Put
	inMsg, err = mc.InboundPut(handler.ctx.ChainID, 0, ids.GenerateTestID(), nil)
	assert.NoError(t, err)
	chainRouter.HandleInbound(inMsg, vID, nil)

	// MultiPut
	inMsg, err = mc.InboundMultiPut(handler.ctx.ChainID, 1, nil)
	assert.NoError(t, err)
	chainRouter.HandleInbound(inMsg, vID, nil)

	// Chits # 1
	inMsg, err = mc.InboundChits(handler.ctx.ChainID, 2, nil)
	assert.NoError(t, err)
	chainRouter.HandleInbound(inMsg, vID, nil)

	// Chits # 2
	inMsg, err = mc.InboundChits(handler.ctx.ChainID, 3, nil)
	assert.NoError(t, err)
	chainRouter.HandleInbound(inMsg, vID, nil)

	// Accepted
	inMsg, err = mc.InboundAccepted(handler.ctx.ChainID, 4, nil)
	assert.NoError(t, err)
	chainRouter.HandleInbound(inMsg, vID, nil)

	// Accepted Frontier
	inMsg, err = mc.InboundAcceptedFrontier(handler.ctx.ChainID, 5, nil)
	assert.NoError(t, err)
	chainRouter.HandleInbound(inMsg, vID, nil)

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
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Millisecond, ids.Set{}, nil, HealthConfig{}, "", prometheus.NewRegistry())
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

	engine.ContextF = func() *snow.Context {
		ctx := snow.DefaultContextTest()
		ctx.SetValidatorOnly()
		return ctx
	}

	handler := &Handler{}
	metrics := prometheus.NewRegistry()
	vdrs := validators.NewSet()
	vID := ids.GenerateTestShortID()
	err = vdrs.AddWeight(vID, 1)
	assert.NoError(t, err)
	err = handler.Initialize(
		&engine,
		vdrs,
		nil,
		"",
		metrics,
	)
	assert.NoError(t, err)

	chainRouter.AddChain(handler)
	go handler.Dispatch()

	// create messages builder
	mc, err := message.NewMsgCreator(metrics, true /*compress*/)
	assert.NoError(t, err)

	// generate a non-validator ID
	nID := ids.GenerateTestShortID()

	*calledF = false
	timeout := chainRouter.clock.Time().Add(time.Hour)
	var inMsg message.InboundMessage

	inMsg, err = mc.InboundPullQuery(handler.ctx.ChainID, uint32(1), uint64(timeout.Unix()), ids.GenerateTestID())
	assert.NoError(t, err)
	chainRouter.HandleInbound(inMsg, nID, func() {})
	assert.False(t, *calledF) // should not be called

	// validator case
	*calledF = false
	wg.Add(1)
	inMsg, err = mc.InboundPullQuery(handler.ctx.ChainID, uint32(2), uint64(timeout.Unix()), ids.GenerateTestID())
	assert.NoError(t, err)
	chainRouter.HandleInbound(inMsg, vID, func() {})

	wg.Wait()
	// should be called since this is a validator request
	assert.True(t, *calledF)

	// register a validator request
	reqID := uint32(3)
	chainRouter.RegisterRequest(vID, handler.ctx.ChainID, reqID, constants.GetMsg)
	assert.Equal(t, 1, chainRouter.timedRequests.Len())

	// remove it from validators
	err = handler.validators.Set(validators.NewSet().List())
	assert.NoError(t, err)
	calledFinished := false
	inMsg, err = mc.InboundPut(handler.ctx.ChainID, reqID, ids.GenerateTestID(), nil)
	assert.NoError(t, err)
	chainRouter.HandleInbound(inMsg, nID, func() { calledFinished = true })

	assert.True(t, calledFinished)
	// shouldn't clear out timed request, as the request should be cleared when
	// the GetFailed message is sent
	assert.Equal(t, 1, chainRouter.timedRequests.Len())
}
