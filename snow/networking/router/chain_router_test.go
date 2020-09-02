// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/networking/throttler"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/timer"
)

func TestShutdown(t *testing.T) {
	tm := timeout.Manager{}
	tm.Initialize(&timer.AdaptiveTimeoutConfig{
		InitialTimeout:    10 * time.Second,
		MinimumTimeout:    500 * time.Millisecond,
		MaximumTimeout:    10 * time.Second,
		TimeoutMultiplier: 1.1,
		TimeoutReduction:  time.Millisecond,
		Namespace:         "",
		Registerer:        prometheus.NewRegistry(),
	})
	go tm.Dispatch()

	chainRouter := ChainRouter{}
	chainRouter.Initialize(logging.NoLog{}, &tm, time.Hour, time.Second)

	engine := common.EngineTest{T: t}
	engine.Default(false)

	shutdownCalled := make(chan struct{}, 1)

	engine.ContextF = snow.DefaultContextTest
	engine.ShutdownF = func() error { shutdownCalled <- struct{}{}; return nil }

	handler := &Handler{}
	handler.Initialize(
		&engine,
		validators.NewSet(),
		nil,
		1,
		throttler.DefaultMaxNonStakerPendingMsgs,
		throttler.DefaultStakerPortion,
		throttler.DefaultStakerPortion,
		"",
		prometheus.NewRegistry(),
	)

	go handler.Dispatch()

	chainRouter.AddChain(handler)

	chainRouter.Shutdown()

	ticker := time.NewTicker(20 * time.Millisecond)
	select {
	case <-ticker.C:
		t.Fatalf("Handler shutdown was not called or timed out after 20ms during chainRouter shutdown")
	case <-shutdownCalled:
	}

	select {
	case <-handler.closed:
	default:
		t.Fatal("handler shutdown but never closed its closing channel")
	}
}

func TestShutdownTimesOut(t *testing.T) {
	tm := timeout.Manager{}
	// Ensure that the MultiPut request does not timeout
	tm.Initialize(&timer.AdaptiveTimeoutConfig{
		InitialTimeout:    10 * time.Second,
		MinimumTimeout:    500 * time.Millisecond,
		MaximumTimeout:    10 * time.Second,
		TimeoutMultiplier: 1.1,
		TimeoutReduction:  time.Millisecond,
		Namespace:         "",
		Registerer:        prometheus.NewRegistry(),
	})
	go tm.Dispatch()

	chainRouter := ChainRouter{}
	chainRouter.Initialize(logging.NoLog{}, &tm, time.Hour, time.Millisecond)

	engine := common.EngineTest{T: t}
	engine.Default(false)

	engineFinished := make(chan struct{}, 1)

	// MultiPut blocks for two seconds
	engine.MultiPutF = func(validatorID ids.ShortID, requestID uint32, containers [][]byte) error {
		time.Sleep(2 * time.Second)
		engineFinished <- struct{}{}
		return nil
	}

	closed := new(int)

	engine.ContextF = snow.DefaultContextTest
	engine.ShutdownF = func() error { *closed++; return nil }

	handler := &Handler{}
	handler.Initialize(
		&engine,
		validators.NewSet(),
		nil,
		1,
		throttler.DefaultMaxNonStakerPendingMsgs,
		throttler.DefaultStakerPortion,
		throttler.DefaultStakerPortion,
		"",
		prometheus.NewRegistry(),
	)

	chainRouter.AddChain(handler)

	go handler.Dispatch()

	shutdownFinished := make(chan struct{}, 1)

	go func() {
		handler.MultiPut(ids.NewShortID([20]byte{}), 1, nil)
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
