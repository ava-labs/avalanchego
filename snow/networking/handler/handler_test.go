// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
)

var errFatal = errors.New("error should cause handler to close")

func TestHandlerDropsTimedOutMessages(t *testing.T) {
	called := make(chan struct{})

	ctx := snow.DefaultConsensusContextTest()

	vdrs := validators.NewSet()
	vdr0 := ids.GenerateTestNodeID()
	err := vdrs.Add(vdr0, nil, ids.Empty, 1)
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handlerIntf, err := New(
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
	)
	require.NoError(t, err)
	handler := handlerIntf.(*handler)

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
	bootstrapper.GetAcceptedFrontierF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
		t.Fatalf("GetAcceptedFrontier message should have timed out")
		return nil
	}
	bootstrapper.GetAcceptedF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
		called <- struct{}{}
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping, // assumed bootstrap is ongoing
	})

	pastTime := time.Now()
	handler.clock.Set(pastTime)

	nodeID := ids.EmptyNodeID
	reqID := uint32(1)
	chainID := ids.ID{}
	msg := message.InboundGetAcceptedFrontier(chainID, reqID, 0*time.Second, nodeID, p2p.EngineType_ENGINE_TYPE_SNOWMAN)
	handler.Push(context.Background(), msg)

	currentTime := time.Now().Add(time.Second)
	handler.clock.Set(currentTime)

	reqID++
	msg = message.InboundGetAccepted(chainID, reqID, 1*time.Second, nil, nodeID, p2p.EngineType_ENGINE_TYPE_SNOWMAN)
	handler.Push(context.Background(), msg)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	select {
	case <-ticker.C:
		t.Fatalf("Calling engine function timed out")
	case <-called:
	}
}

func TestHandlerClosesOnError(t *testing.T) {
	closed := make(chan struct{}, 1)
	ctx := snow.DefaultConsensusContextTest()

	vdrs := validators.NewSet()
	err := vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handlerIntf, err := New(
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
	)
	require.NoError(t, err)
	handler := handlerIntf.(*handler)

	handler.clock.Set(time.Now())
	handler.SetOnStopped(func() {
		closed <- struct{}{}
	})

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
	bootstrapper.GetAcceptedFrontierF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
		return errFatal
	}
	handler.SetBootstrapper(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	handler.SetConsensus(engine)

	// assume bootstrapping is ongoing so that InboundGetAcceptedFrontier
	// should normally be handled
	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping,
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	nodeID := ids.EmptyNodeID
	reqID := uint32(1)
	deadline := time.Nanosecond
	msg := message.InboundGetAcceptedFrontier(ids.ID{}, reqID, deadline, nodeID, 0)
	handler.Push(context.Background(), msg)

	ticker := time.NewTicker(time.Second)
	select {
	case <-ticker.C:
		t.Fatalf("Handler shutdown timed out before calling toClose")
	case <-closed:
	}
}

func TestHandlerDropsGossipDuringBootstrapping(t *testing.T) {
	closed := make(chan struct{}, 1)
	ctx := snow.DefaultConsensusContextTest()
	vdrs := validators.NewSet()
	err := vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handlerIntf, err := New(
		ctx,
		vdrs,
		nil,
		nil,
		1,
		resourceTracker,
		validators.UnhandledSubnetConnector,
	)
	require.NoError(t, err)
	handler := handlerIntf.(*handler)

	handler.clock.Set(time.Now())

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
	bootstrapper.GetFailedF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
		closed <- struct{}{}
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping, // assumed bootstrap is ongoing
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	nodeID := ids.EmptyNodeID
	chainID := ids.Empty
	reqID := uint32(1)
	inMsg := message.InternalGetFailed(nodeID, chainID, reqID, p2p.EngineType_ENGINE_TYPE_SNOWMAN)
	handler.Push(context.Background(), inMsg)

	ticker := time.NewTicker(time.Second)
	select {
	case <-ticker.C:
		t.Fatalf("Handler shutdown timed out before calling toClose")
	case <-closed:
	}
}

// Test that messages from the VM are handled
func TestHandlerDispatchInternal(t *testing.T) {
	calledNotify := make(chan struct{}, 1)
	ctx := snow.DefaultConsensusContextTest()
	msgFromVMChan := make(chan common.Message)
	vdrs := validators.NewSet()
	err := vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handler, err := New(
		ctx,
		vdrs,
		msgFromVMChan,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
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
	handler.SetBootstrapper(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	engine.NotifyF = func(context.Context, common.Message) error {
		calledNotify <- struct{}{}
		return nil
	}
	handler.SetConsensus(engine)
	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp, // assumed bootstrap is done
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)
	msgFromVMChan <- 0

	select {
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("should have called notify")
	case <-calledNotify:
	}
}

func TestHandlerSubnetConnector(t *testing.T) {
	ctx := snow.DefaultConsensusContextTest()
	vdrs := validators.NewSet()
	err := vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	connector := validators.NewMockSubnetConnector(ctrl)

	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()

	require.NoError(t, err)
	handler, err := New(
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
		connector,
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
	handler.SetBootstrapper(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	handler.SetConsensus(engine)
	ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp, // assumed bootstrap is done
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	// Handler should call subnet connector when ConnectedSubnet message is received
	var wg sync.WaitGroup
	connector.EXPECT().ConnectedSubnet(gomock.Any(), nodeID, subnetID).Do(
		func(context.Context, ids.NodeID, ids.ID) {
			wg.Done()
		})

	wg.Add(1)
	defer wg.Wait()

	subnetMsg := message.InternalConnectedSubnet(nodeID, subnetID)
	handler.Push(context.Background(), subnetMsg)
}
