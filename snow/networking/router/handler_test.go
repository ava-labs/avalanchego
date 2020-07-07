// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/snow/networking/timeout"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/prometheus/client_golang/prometheus"
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
	handler.clock.Set(receiveTime)

	handler.GetAcceptedFrontier(ids.NewShortID([20]byte{}), 1)
	// Set the clock to simulate message timeout
	handler.clock.Set(receiveTime.Add(2 * timeout.DefaultRequestTimeout))
	handler.GetAccepted(ids.NewShortID([20]byte{}), 1, ids.Set{})

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

	handler.GetAcceptedFrontier(ids.NewShortID([20]byte{}), 1)
	go handler.Dispatch()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	select {
	case _, _ = <-ticker.C:
		t.Fatalf("Calling engine function timed out")
	case _, _ = <-called:
	}
}
