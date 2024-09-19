// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	commontracker "github.com/ava-labs/avalanchego/snow/engine/common/tracker"
)

const testThreadPoolSize = 2

var errFatal = errors.New("error should cause handler to close")

func TestHandlerDropsTimedOutMessages(t *testing.T) {
	require := require.New(t)

	called := make(chan struct{})

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)

	vdrs := validators.NewManager()
	vdr0 := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdr0, nil, ids.Empty, 1))

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	handlerIntf, err := New(
		ctx,
		vdrs,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)
	handler := handlerIntf.(*handler)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.GetAcceptedFrontierF = func(context.Context, ids.NodeID, uint32) error {
		require.FailNow("GetAcceptedFrontier message should have timed out")
		return nil
	}
	bootstrapper.GetAcceptedF = func(context.Context, ids.NodeID, uint32, set.Set[ids.ID]) error {
		called <- struct{}{}
		return nil
	}
	handler.SetEngine(bootstrapper)

	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping, // assumed bootstrap is ongoing
	})

	pastTime := time.Now()
	handler.clock.Set(pastTime)

	nodeID := ids.EmptyNodeID
	reqID := uint32(1)
	chainID := ids.Empty
	msg := Message{
		InboundMessage: message.InboundGetAcceptedFrontier(chainID, reqID, 0*time.Second, nodeID),
		EngineType:     p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	handler.Push(context.Background(), msg)

	currentTime := time.Now().Add(time.Second)
	handler.clock.Set(currentTime)

	reqID++
	msg = Message{
		InboundMessage: message.InboundGetAccepted(chainID, reqID, 1*time.Second, nil, nodeID),
		EngineType:     p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	handler.Push(context.Background(), msg)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	select {
	case <-ticker.C:
		require.FailNow("Calling engine function timed out")
	case <-called:
	}
}

func TestHandlerClosesOnError(t *testing.T) {
	require := require.New(t)

	closed := make(chan struct{}, 1)
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

	peerTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	handlerIntf, err := New(
		ctx,
		vdrs,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)
	handler := handlerIntf.(*handler)

	handler.clock.Set(time.Now())
	handler.SetOnStopped(func() {
		closed <- struct{}{}
	})

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.GetAcceptedFrontierF = func(context.Context, ids.NodeID, uint32) error {
		return errFatal
	}

	engine := &enginetest.Engine{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}

	handler.SetEngine(bootstrapper)

	// assume bootstrapping is ongoing so that InboundGetAcceptedFrontier
	// should normally be handled
	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping,
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	nodeID := ids.EmptyNodeID
	reqID := uint32(1)
	deadline := time.Nanosecond
	msg := Message{
		InboundMessage: message.InboundGetAcceptedFrontier(ids.Empty, reqID, deadline, nodeID),
		EngineType:     p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	handler.Push(context.Background(), msg)

	ticker := time.NewTicker(time.Second)
	select {
	case <-ticker.C:
		require.FailNow("Handler shutdown timed out before calling toClose")
	case <-closed:
	}
}

func TestHandlerDropsGossipDuringBootstrapping(t *testing.T) {
	require := require.New(t)

	closed := make(chan struct{}, 1)
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

	peerTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	handlerIntf, err := New(
		ctx,
		vdrs,
		nil,
		1,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)
	handler := handlerIntf.(*handler)

	handler.clock.Set(time.Now())

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.GetFailedF = func(context.Context, ids.NodeID, uint32) error {
		closed <- struct{}{}
		return nil
	}

	handler.SetEngine(bootstrapper)
	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping, // assumed bootstrap is ongoing
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	nodeID := ids.EmptyNodeID
	chainID := ids.Empty
	reqID := uint32(1)
	inInboundMessage := Message{
		InboundMessage: message.InternalGetFailed(nodeID, chainID, reqID),
		EngineType:     p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	handler.Push(context.Background(), inInboundMessage)

	ticker := time.NewTicker(time.Second)
	select {
	case <-ticker.C:
		require.FailNow("Handler shutdown timed out before calling toClose")
	case <-closed:
	}
}

// Test that messages from the VM are handled
func TestHandlerDispatchInternal(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	msgFromVMChan := make(chan common.Message)
	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	handler, err := New(
		ctx,
		vdrs,
		msgFromVMChan,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
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

	engine := &enginetest.Engine{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}

	wg := &sync.WaitGroup{}
	engine.NotifyF = func(context.Context, common.Message) error {
		wg.Done()
		return nil
	}

	handler.SetEngine(engine)

	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp, // assumed bootstrap is done
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	wg.Add(1)
	handler.Start(context.Background(), false)
	msgFromVMChan <- 0
	wg.Wait()
}

// Tests that messages are routed to the correct engine type
func TestDynamicEngineTypeDispatch(t *testing.T) {
	require := require.New(t)

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

	peerTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	handler, err := New(
		ctx,
		vdrs,
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ids.EmptyNodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
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

	engine := &enginetest.Engine{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}

	appReqChan := make(chan struct{})
	chitsChan := make(chan struct{}, 1)

	engine.AppRequestF = func(context.Context, ids.NodeID, uint32, time.Time, []byte) error {
		// Wait for chits message to be dispatched before proceeding with processing
		<-chitsChan
		close(appReqChan)
		return nil
	}
	engine.ChitsF = func(context.Context, ids.NodeID, uint32, ids.ID, ids.ID, ids.ID, uint64) error {
		chitsChan <- struct{}{}
		return nil
	}

	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp,
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.SetEngine(engine)
	handler.Start(context.Background(), false)

	// Ensure app requests are dispatched on a different goroutine than the chits.
	// Dispatch app request first, but have it wait for the chits message to be processed.
	// If they are dispatched on the same goroutine, a deadlock would occur.
	handler.Push(context.Background(), Message{
		InboundMessage: message.InboundAppRequest(ids.Empty, uint32(0), time.Hour, nil, ids.EmptyNodeID),
		EngineType:     p2ppb.EngineType_ENGINE_TYPE_SNOWMAN,
	})
	handler.Push(context.Background(), Message{
		InboundMessage: message.InboundChits(
			ids.Empty,
			uint32(0),
			ids.Empty,
			ids.Empty,
			ids.Empty,
			ids.EmptyNodeID,
		),
		EngineType: p2ppb.EngineType_ENGINE_TYPE_SNOWMAN,
	})

	<-appReqChan
}

func TestHandlerStartError(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	handler, err := New(
		ctx,
		validators.NewManager(),
		nil,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	// Starting a handler with an unprovided engine should immediately cause the
	// handler to shutdown.

	handler.SetEngine(&enginetest.Engine{StartF: func(_ context.Context, _ uint32) error {
		return errors.New("oops")
	}})

	ctx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Initializing,
	})
	handler.Start(context.Background(), false)

	_, err = handler.AwaitStopped(context.Background())
	require.NoError(err)
}
