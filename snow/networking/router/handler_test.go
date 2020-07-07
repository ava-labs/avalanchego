// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/prometheus/client_golang/prometheus"
)

func TestHandlerDropsTimedOutMessages(t *testing.T) {
	engine := common.EngineTest{T: t}
	// Message should be dropped, so if an engine function is called
	// the test should fail
	engine.Default(true)
	engine.ContextF = snow.DefaultContextTest

	handler := &Handler{}
	handler.Initialize(
		&engine,
		nil,
		1,
		"",
		prometheus.NewRegistry(),
	)

	handler.dropMessageTimeout = time.Millisecond

	handler.GetAcceptedFrontier(ids.NewShortID([20]byte{}), 1)
	// Sleep past the message timeout
	time.Sleep(4 * time.Millisecond)
	go handler.Dispatch()

	// Ensure handler has time to process the message and trigger
	// failure if the message was not dropped
	time.Sleep(50 * time.Millisecond)
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
