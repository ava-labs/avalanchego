// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"math/rand"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

func TestSenderContext(t *testing.T) {
	context := snow.DefaultContextTest()
	sender := Sender{}
	sender.Initialize(
		context,
		&ExternalSenderTest{},
		&router.ChainRouter{},
		&timeout.Manager{},
	)
	if res := sender.Context(); !reflect.DeepEqual(res, context) {
		t.Fatalf("Got %#v, expected %#v", res, context)
	}
}

func TestTimeout(t *testing.T) {
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

	chainRouter := router.ChainRouter{}
	chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Second, ids.Set{}, nil)

	sender := Sender{}
	sender.Initialize(snow.DefaultContextTest(), &ExternalSenderTest{}, &chainRouter, &tm)

	engine := common.EngineTest{T: t}
	engine.Default(true)
	engine.CantConnected = false

	engine.ContextF = snow.DefaultContextTest

	wg := sync.WaitGroup{}
	wg.Add(2)

	failedVDRs := ids.ShortSet{}
	engine.QueryFailedF = func(validatorID ids.ShortID, _ uint32) error {
		failedVDRs.Add(validatorID)
		wg.Done()
		return nil
	}

	handler := router.Handler{}
	handler.Initialize(
		&engine,
		vdrs,
		nil,
		1,
		router.DefaultMaxNonStakerPendingMsgs,
		router.DefaultStakerPortion,
		router.DefaultStakerPortion,
		"",
		prometheus.NewRegistry(),
	)
	go handler.Dispatch()

	chainRouter.AddChain(&handler)

	vdrIDs := ids.ShortSet{}
	vdrIDs.Add(ids.NewShortID([20]byte{255}))
	vdrIDs.Add(ids.NewShortID([20]byte{254}))

	sender.PullQuery(vdrIDs, 0, ids.Empty)

	wg.Wait()

	if !failedVDRs.Equals(vdrIDs) {
		t.Fatalf("Timeouts should have fired")
	}
}

func TestReliableMessages(t *testing.T) {
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

	chainRouter := router.ChainRouter{}
	chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Second, ids.Set{}, nil)

	sender := Sender{}
	sender.Initialize(snow.DefaultContextTest(), &ExternalSenderTest{}, &chainRouter, &tm)

	engine := common.EngineTest{T: t}
	engine.Default(true)
	engine.CantConnected = false

	engine.ContextF = snow.DefaultContextTest
	engine.GossipF = func() error { return nil }

	queriesToSend := 1000
	awaiting := make([]chan struct{}, queriesToSend)
	for i := 0; i < queriesToSend; i++ {
		awaiting[i] = make(chan struct{}, 1)
	}

	engine.QueryFailedF = func(validatorID ids.ShortID, reqID uint32) error {
		close(awaiting[int(reqID)])
		return nil
	}

	handler := router.Handler{}
	handler.Initialize(
		&engine,
		vdrs,
		nil,
		1,
		router.DefaultMaxNonStakerPendingMsgs,
		router.DefaultStakerPortion,
		router.DefaultStakerPortion,
		"",
		prometheus.NewRegistry(),
	)
	go handler.Dispatch()

	chainRouter.AddChain(&handler)

	go func() {
		for i := 0; i < queriesToSend; i++ {
			vdrIDs := ids.ShortSet{}
			vdrIDs.Add(ids.NewShortID([20]byte{1}))

			sender.PullQuery(vdrIDs, uint32(i), ids.Empty)
			time.Sleep(time.Duration(rand.Float64() * float64(time.Microsecond))) // #nosec G404
		}
	}()

	go func() {
		for {
			chainRouter.Gossip()
			time.Sleep(time.Duration(rand.Float64() * float64(time.Microsecond))) // #nosec G404
		}
	}()

	for _, await := range awaiting {
		<-await
	}
}

func TestReliableMessagesToMyself(t *testing.T) {
	benchlist := benchlist.NewNoBenchlist()
	vdrs := validators.NewSet()
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

	chainRouter := router.ChainRouter{}
	chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &tm, time.Hour, time.Second, ids.Set{}, nil)

	sender := Sender{}
	sender.Initialize(snow.DefaultContextTest(), &ExternalSenderTest{}, &chainRouter, &tm)

	engine := common.EngineTest{T: t}
	engine.Default(false)

	engine.ContextF = snow.DefaultContextTest
	engine.GossipF = func() error { return nil }
	engine.CantPullQuery = false

	queriesToSend := 2
	awaiting := make([]chan struct{}, queriesToSend)
	for i := 0; i < queriesToSend; i++ {
		awaiting[i] = make(chan struct{}, 1)
	}

	engine.QueryFailedF = func(validatorID ids.ShortID, reqID uint32) error {
		close(awaiting[int(reqID)])
		return nil
	}

	handler := router.Handler{}
	handler.Initialize(
		&engine,
		vdrs,
		nil,
		1,
		router.DefaultMaxNonStakerPendingMsgs,
		router.DefaultStakerPortion,
		router.DefaultStakerPortion,
		"",
		prometheus.NewRegistry(),
	)
	go handler.Dispatch()

	chainRouter.AddChain(&handler)

	go func() {
		for i := 0; i < queriesToSend; i++ {
			vdrIDs := ids.ShortSet{}
			vdrIDs.Add(engine.Context().NodeID)

			sender.PullQuery(vdrIDs, uint32(i), ids.Empty)
			time.Sleep(time.Duration(rand.Float64() * float64(time.Microsecond))) // #nosec G404
		}
	}()

	go func() {
		for {
			chainRouter.Gossip()
			time.Sleep(time.Duration(rand.Float64() * float64(time.Microsecond))) // #nosec G404
		}
	}()

	for _, await := range awaiting {
		<-await
	}
}
