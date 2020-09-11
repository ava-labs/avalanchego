// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/throttler"
	"github.com/ava-labs/avalanchego/snow/validators"
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
	vdrs := validators.NewSet()
	vdr0 := ids.GenerateTestShortID()
	vdrs.AddWeight(vdr0, 1)
	handler.Initialize(
		&engine,
		vdrs,
		nil,
		16,
		throttler.DefaultMaxNonStakerPendingMsgs,
		throttler.DefaultStakerPortion,
		throttler.DefaultStakerPortion,
		"",
		prometheus.NewRegistry(),
	)

	currentTime := time.Now()
	handler.clock.Set(currentTime)

	handler.GetAcceptedFrontier(ids.NewShortID([20]byte{}), 1, currentTime.Add(-time.Second))
	handler.GetAccepted(ids.NewShortID([20]byte{}), 1, currentTime.Add(time.Second), ids.Set{})

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

	engine.GetAcceptedFrontierF = func(validatorID ids.ShortID, requestID uint32) error {
		called <- struct{}{}
		return nil
	}

	handler := &Handler{}
	validators := validators.NewSet()
	handler.Initialize(
		&engine,
		validators,
		nil,
		16,
		throttler.DefaultMaxNonStakerPendingMsgs,
		throttler.DefaultStakerPortion,
		throttler.DefaultStakerPortion,
		"",
		prometheus.NewRegistry(),
	)

	handler.GetAcceptedFrontier(ids.NewShortID([20]byte{}), 1, time.Time{})
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
	engine.GetAcceptedFrontierF = func(validatorID ids.ShortID, requestID uint32) error {
		return errors.New("Engine error should cause handler to close")
	}

	handler := &Handler{}
	handler.Initialize(
		&engine,
		validators.NewSet(),
		nil,
		16,
		throttler.DefaultMaxNonStakerPendingMsgs,
		throttler.DefaultStakerPortion,
		throttler.DefaultStakerPortion,
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
	case <-ticker.C:
		t.Fatalf("Handler shutdown timed out before calling toClose")
	case <-closed:
	}
}
