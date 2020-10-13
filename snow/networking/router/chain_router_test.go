// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
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
	benchlist := benchlist.NewNoBenchlist()
	tm := timeout.Manager{}
	err := tm.Initialize(&timer.AdaptiveTimeoutConfig{
		InitialTimeout: time.Millisecond,
		MinimumTimeout: time.Millisecond,
		MaximumTimeout: 10 * time.Second,
		TimeoutInc:     2 * time.Millisecond,
		TimeoutDec:     time.Millisecond,
		Namespace:      "",
		Registerer:     prometheus.NewRegistry(),
	}, benchlist)
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	chainRouter := ChainRouter{}
	chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Second, ids.Set{}, nil)

	engine := common.EngineTest{T: t}
	engine.Default(false)

	shutdownCalled := make(chan struct{}, 1)

	engine.ContextF = snow.DefaultContextTest
	engine.ShutdownF = func() error { shutdownCalled <- struct{}{}; return nil }

	handler := &Handler{}
	handler.Initialize(
		&engine,
		vdrs,
		nil,
		1,
		DefaultMaxNonStakerPendingMsgs,
		DefaultStakerPortion,
		DefaultStakerPortion,
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
	vdrs := validators.NewSet()
	benchlist := benchlist.NewNoBenchlist()
	tm := timeout.Manager{}
	// Ensure that the MultiPut request does not timeout
	err := tm.Initialize(&timer.AdaptiveTimeoutConfig{
		InitialTimeout: time.Second,
		MinimumTimeout: 500 * time.Millisecond,
		MaximumTimeout: 10 * time.Second,
		TimeoutInc:     2 * time.Millisecond,
		TimeoutDec:     time.Millisecond,
		Namespace:      "",
		Registerer:     prometheus.NewRegistry(),
	}, benchlist)
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	chainRouter := ChainRouter{}
	chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Millisecond, ids.Set{}, nil)

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
		vdrs,
		nil,
		1,
		DefaultMaxNonStakerPendingMsgs,
		DefaultStakerPortion,
		DefaultStakerPortion,
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
