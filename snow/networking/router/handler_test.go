// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
)

func TestHandlerDropsTimedOutMessages(t *testing.T) {
	engine := common.EngineTest{T: t}
	engine.Default(true)
	engine.ContextF = snow.DefaultContextTest
	called := make(chan struct{})

	engine.GetAcceptedFrontierF = func(validatorID ids.ShortID, requestID uint32) error {
		t.Fatalf("GetAcceptedFrontier message should have timed out")
		return nil
	}

	engine.GetAcceptedF = func(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) error {
		called <- struct{}{}
		return nil
	}

	handler := &Handler{}
	handler.Initialize(
		&engine,
		nil,
		2,
		"",
		prometheus.NewRegistry(),
	)

	receiveTime := time.Now()
	handler.clock.Set(receiveTime.Add(time.Second))

	handler.GetAcceptedFrontier(ids.NewShortID([20]byte{}), 1, receiveTime)
	// Set the clock to simulate message timeout
	handler.GetAccepted(ids.NewShortID([20]byte{}), 1, receiveTime.Add(2*time.Second), ids.Set{})

	go handler.Dispatch()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	select {
	case _, _ = <-ticker.C:
		t.Fatalf("Calling engine function timed out")
	case _, _ = <-called:
	}
}

func TestHandlerDoesntDrop(t *testing.T) {
	engine := common.EngineTest{T: t}
	engine.Default(false)
	engine.ContextF = snow.DefaultContextTest

	called := make(chan struct{}, 1)

	engine.GetAcceptedFrontierF = func(validatorID ids.ShortID, requestID uint32) error {
		called <- struct{}{}
		return nil
	}

	handler := &Handler{}
	handler.Initialize(
		&engine,
		nil,
		1,
		"",
		prometheus.NewRegistry(),
	)

	handler.GetAcceptedFrontier(ids.NewShortID([20]byte{}), 1, time.Time{})
	go handler.Dispatch()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	select {
	case _, _ = <-ticker.C:
		t.Fatalf("Calling engine function timed out")
	case _, _ = <-called:
	}
}

func TestHandlerCallsToClose(t *testing.T) {
	engine := common.EngineTest{T: t}
	engine.Default(false)

	closed := make(chan struct{}, 1)

	engine.ContextF = snow.DefaultContextTest
	engine.GetAcceptedFrontierF = func(validatorID ids.ShortID, requestID uint32) error {
		return errors.New("Engine error should cause handler to close")
	}

	handler := &Handler{}
	handler.Initialize(
		&engine,
		nil,
		1,
		"",
		prometheus.NewRegistry(),
	)
	handler.clock.Set(time.Now())

	handler.toClose = func() {
		closed <- struct{}{}
	}
	go handler.Dispatch()

	handler.GetAcceptedFrontier(ids.NewShortID([20]byte{}), 1, time.Now().Add(time.Second))

	ticker := time.NewTicker(20 * time.Millisecond)
	select {
	case _, _ = <-ticker.C:
		t.Fatalf("Handler shutdown timed out before calling toClose")
	case _, _ = <-closed:
	}
}
