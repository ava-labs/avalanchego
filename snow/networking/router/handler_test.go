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
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestHandlerDropsTimedOutMessages(t *testing.T) {
	engine := common.EngineTest{T: t}
	engine.Default(true)
	engine.ContextF = snow.DefaultContextTest
	called := make(chan struct{})

	engine.GetAcceptedFrontierF = func(nodeID ids.ShortID, requestID uint32) error {
		t.Fatalf("GetAcceptedFrontier message should have timed out")
		return nil
	}

	engine.GetAcceptedF = func(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
		called <- struct{}{}
		return nil
	}

	handler := &Handler{}
	vdrs := validators.NewSet()
	vdr0 := ids.GenerateTestShortID()
	if err := vdrs.AddWeight(vdr0, 1); err != nil {
		t.Fatal(err)
	}
	err := handler.Initialize(
		&engine,
		vdrs,
		nil,
		"",
		prometheus.NewRegistry(),
	)
	assert.NoError(t, err)

	currentTime := time.Now()
	handler.clock.Set(currentTime)

	handler.GetAcceptedFrontier(ids.ShortID{}, 1, currentTime.Add(-time.Second), func() {})
	handler.GetAccepted(ids.ShortID{}, 1, currentTime.Add(time.Second), nil, func() {})

	go handler.Dispatch()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	select {
	case <-ticker.C:
		t.Fatalf("Calling engine function timed out")
	case <-called:
	}
}

func TestHandlerDoesntDrop(t *testing.T) {
	engine := common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = snow.DefaultContextTest

	called := make(chan struct{}, 1)

	engine.GetAcceptedFrontierF = func(nodeID ids.ShortID, requestID uint32) error {
		called <- struct{}{}
		return nil
	}

	handler := &Handler{}
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	err = handler.Initialize(
		&engine,
		vdrs,
		nil,
		"",
		prometheus.NewRegistry(),
	)
	assert.NoError(t, err)

	handler.GetAcceptedFrontier(ids.ShortID{}, 1, time.Time{}, func() {})
	go handler.Dispatch()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	select {
	case <-ticker.C:
		t.Fatalf("Calling engine function timed out")
	case <-called:
	}
}

func TestHandlerClosesOnError(t *testing.T) {
	engine := common.EngineTest{T: t}
	engine.Default(false)

	closed := make(chan struct{}, 1)

	engine.ContextF = snow.DefaultContextTest
	engine.GetAcceptedFrontierF = func(nodeID ids.ShortID, requestID uint32) error {
		return errors.New("Engine error should cause handler to close")
	}

	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
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

	handler.clock.Set(time.Now())

	handler.onCloseF = func() {
		closed <- struct{}{}
	}
	go handler.Dispatch()

	handler.GetAcceptedFrontier(ids.ShortID{}, 1, time.Now().Add(time.Second), func() {})

	ticker := time.NewTicker(20 * time.Millisecond)
	select {
	case <-ticker.C:
		t.Fatalf("Handler shutdown timed out before calling toClose")
	case <-closed:
	}
}

func TestHandlerDropsGossipDuringBootstrapping(t *testing.T) {
	engine := common.EngineTest{T: t}
	engine.Default(false)

	engine.CantGossip = true

	closed := make(chan struct{}, 1)

	engine.ContextF = snow.DefaultContextTest
	engine.GetFailedF = func(nodeID ids.ShortID, requestID uint32) error {
		closed <- struct{}{}
		return nil
	}

	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
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

	handler.clock.Set(time.Now())

	go handler.Dispatch()

	handler.Gossip()
	handler.GetFailed(ids.ShortEmpty, 1)

	ticker := time.NewTicker(20 * time.Millisecond)
	select {
	case <-ticker.C:
		t.Fatalf("Handler shutdown timed out before calling toClose")
	case <-closed:
	}
}

// Test that messages from the VM are handled
func TestHandlerDispatchInternal(t *testing.T) {
	engine := common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = snow.DefaultContextTest
	calledNotify := make(chan struct{}, 1)
	engine.NotifyF = func(common.Message) error {
		calledNotify <- struct{}{}
		return nil
	}

	handler := &Handler{}
	msgFromVMChan := make(chan common.Message)
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	err = handler.Initialize(
		&engine,
		vdrs,
		msgFromVMChan,
		"",
		prometheus.NewRegistry(),
	)
	assert.NoError(t, err)

	go handler.Dispatch()
	msgFromVMChan <- 0

	select {
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("should have called notify")
	case <-calledNotify:
	}
}
