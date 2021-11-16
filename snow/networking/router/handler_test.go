// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestHandlerDropsTimedOutMessages(t *testing.T) {
	called := make(chan struct{})

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()

	vdrs := validators.NewSet()
	vdr0 := ids.GenerateTestShortID()
	err = vdrs.AddWeight(vdr0, 1)
	assert.NoError(t, err)

	handler, err := NewHandler(
		mc,
		ctx,
		vdrs,
		nil,
	)
	assert.NoError(t, err)

	bootstrapper := &common.EngineTest{T: t}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.GetAcceptedFrontierF = func(nodeID ids.ShortID, requestID uint32) error {
		t.Fatalf("GetAcceptedFrontier message should have timed out")
		return nil
	}
	bootstrapper.GetAcceptedF = func(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
		called <- struct{}{}
		return nil
	}
	handler.RegisterBootstrap(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(true)
	handler.RegisterEngine(engine)
	engine.ContextF = func() *snow.ConsensusContext { return ctx }

	pastTime := time.Now()
	mc.SetTime(pastTime)
	handler.clock.Set(pastTime)

	nodeID := ids.ShortEmpty
	reqID := uint32(1)
	deadline := time.Nanosecond
	chainID := ids.ID{}
	msg := mc.InboundGetAcceptedFrontier(chainID, reqID, deadline, nodeID)
	handler.Push(msg)

	currentTime := time.Now().Add(time.Second)
	mc.SetTime(currentTime)
	handler.clock.Set(currentTime)

	reqID++
	msg = mc.InboundGetAccepted(chainID, reqID, deadline, nil, nodeID)
	handler.Push(msg)

	go handler.Dispatch()

	ticker := time.NewTicker(50 * time.Millisecond)
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
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)

	handler, err := NewHandler(
		mc,
		ctx,
		vdrs,
		nil,
	)
	assert.NoError(t, err)

	handler.clock.Set(time.Now())
	handler.onCloseF = func() {
		closed <- struct{}{}
	}

	bootstrapper := &common.EngineTest{T: t}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.GetAcceptedFrontierF = func(nodeID ids.ShortID, requestID uint32) error {
		return errors.New("Engine error should cause handler to close")
	}
	handler.RegisterBootstrap(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext { return ctx }
	handler.RegisterEngine(engine)

	go handler.Dispatch()

	nodeID := ids.ShortEmpty
	reqID := uint32(1)
	deadline := time.Nanosecond
	msg := mc.InboundGetAcceptedFrontier(ids.ID{}, reqID, deadline, nodeID)
	handler.Push(msg)

	ticker := time.NewTicker(20 * time.Millisecond)
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
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)

	handler, err := NewHandler(
		mc,
		ctx,
		vdrs,
		nil,
	)
	assert.NoError(t, err)

	handler.clock.Set(time.Now())

	bootstrapper := &common.EngineTest{T: t}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.GetFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		closed <- struct{}{}
		return nil
	}
	handler.RegisterBootstrap(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext { return ctx }
	engine.CantGossip = true
	handler.RegisterEngine(engine)

	go handler.Dispatch()

	handler.Gossip()

	nodeID := ids.ShortEmpty
	chainID := ids.Empty
	reqID := uint32(1)
	inMsg := mc.InternalFailedRequest(message.GetFailed, nodeID, chainID, reqID)
	handler.Push(inMsg)

	ticker := time.NewTicker(20 * time.Millisecond)
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
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)

	handler, err := NewHandler(
		mc,
		ctx,
		vdrs,
		msgFromVMChan,
	)
	assert.NoError(t, err)

	bootstrapper := &common.EngineTest{T: t}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.NotifyF = func(common.Message) error {
		calledNotify <- struct{}{}
		return nil
	}
	handler.RegisterBootstrap(bootstrapper)

	engine := &common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = func() *snow.ConsensusContext { return ctx }
	handler.RegisterEngine(engine)

	go handler.Dispatch()
	msgFromVMChan <- 0

	select {
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("should have called notify")
	case <-calledNotify:
	}
}
