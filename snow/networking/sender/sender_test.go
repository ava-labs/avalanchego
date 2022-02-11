// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"math/rand"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
)

func TestSenderContext(t *testing.T) {
	context := snow.DefaultConsensusContextTest()
	metrics := prometheus.NewRegistry()
	msgCreator, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(true)
	sender := Sender{}
	err = sender.Initialize(
		context,
		msgCreator,
		externalSender,
		&router.ChainRouter{},
		&timeout.Manager{},
		2,
		2,
		2,
	)
	assert.NoError(t, err)
	if res := sender.Context(); !reflect.DeepEqual(res, context) {
		t.Fatalf("Got %#v, expected %#v", res, context)
	}
}

func TestTimeout(t *testing.T) {
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	benchlist := benchlist.NewNoBenchlist()
	tm := timeout.Manager{}
	err = tm.Initialize(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     time.Millisecond,
			MinimumTimeout:     time.Millisecond,
			MaximumTimeout:     10 * time.Second,
			TimeoutHalflife:    5 * time.Minute,
			TimeoutCoefficient: 1.25,
		},
		benchlist,
		"",
		prometheus.NewRegistry(),
	)
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	chainRouter := router.ChainRouter{}
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Second, ids.Set{}, nil, router.HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	context := snow.DefaultConsensusContextTest()
	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(false)
	sender := Sender{}
	err = sender.Initialize(context, mc, externalSender, &chainRouter, &tm, 2, 2, 2)
	assert.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(2)
	failedVDRs := ids.ShortSet{}
	ctx := snow.DefaultConsensusContextTest()
	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Hour,
	)
	assert.NoError(t, err)

	bootstrapper := &common.BootstrapperTest{
		BootstrapableTest: common.BootstrapableTest{
			T: t,
		},
		EngineTest: common.EngineTest{
			T: t,
		},
	}
	bootstrapper.Default(true)
	bootstrapper.CantGossip = false
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.ConnectedF = func(nodeID ids.ShortID, nodeVersion version.Application) error { return nil }
	bootstrapper.QueryFailedF = func(nodeID ids.ShortID, _ uint32) error {
		failedVDRs.Add(nodeID)
		wg.Done()
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.SetState(snow.Bootstrapping) // assumed bootstrap is ongoing

	chainRouter.AddChain(handler)
	handler.Start(false)

	vdrIDs := ids.ShortSet{}
	vdrIDs.Add(ids.ShortID{255})
	vdrIDs.Add(ids.ShortID{254})

	sender.SendPullQuery(vdrIDs, 0, ids.Empty)

	wg.Wait()

	if !failedVDRs.Equals(vdrIDs) {
		t.Fatalf("Timeouts should have fired")
	}
}

func TestReliableMessages(t *testing.T) {
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.ShortID{1}, 1)
	assert.NoError(t, err)
	benchlist := benchlist.NewNoBenchlist()
	tm := timeout.Manager{}
	err = tm.Initialize(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     time.Millisecond,
			MinimumTimeout:     time.Millisecond,
			MaximumTimeout:     time.Millisecond,
			TimeoutHalflife:    5 * time.Minute,
			TimeoutCoefficient: 1.25,
		},
		benchlist,
		"",
		prometheus.NewRegistry(),
	)
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	chainRouter := router.ChainRouter{}
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Second, ids.Set{}, nil, router.HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	context := snow.DefaultConsensusContextTest()

	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(false)
	sender := Sender{}
	err = sender.Initialize(context, mc, externalSender, &chainRouter, &tm, 2, 2, 2)
	assert.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()

	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		1,
	)
	assert.NoError(t, err)

	bootstrapper := &common.BootstrapperTest{
		BootstrapableTest: common.BootstrapableTest{
			T: t,
		},
		EngineTest: common.EngineTest{
			T: t,
		},
	}
	bootstrapper.Default(true)
	bootstrapper.CantGossip = false
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.ConnectedF = func(nodeID ids.ShortID, nodeVersion version.Application) error { return nil }
	queriesToSend := 1000
	awaiting := make([]chan struct{}, queriesToSend)
	for i := 0; i < queriesToSend; i++ {
		awaiting[i] = make(chan struct{}, 1)
	}
	bootstrapper.QueryFailedF = func(nodeID ids.ShortID, reqID uint32) error {
		close(awaiting[int(reqID)])
		return nil
	}
	bootstrapper.CantGossip = false
	handler.SetBootstrapper(bootstrapper)
	ctx.SetState(snow.Bootstrapping) // assumed bootstrap is ongoing

	chainRouter.AddChain(handler)
	handler.Start(false)

	go func() {
		for i := 0; i < queriesToSend; i++ {
			vdrIDs := ids.ShortSet{}
			vdrIDs.Add(ids.ShortID{1})

			sender.SendPullQuery(vdrIDs, uint32(i), ids.Empty)
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
	err := vdrs.AddWeight(ids.GenerateTestShortID(), 1)
	assert.NoError(t, err)
	tm := timeout.Manager{}
	err = tm.Initialize(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     10 * time.Millisecond,
			MinimumTimeout:     10 * time.Millisecond,
			MaximumTimeout:     10 * time.Millisecond, // Timeout fires immediately
			TimeoutHalflife:    5 * time.Minute,
			TimeoutCoefficient: 1.25,
		},
		benchlist,
		"",
		prometheus.NewRegistry(),
	)
	if err != nil {
		t.Fatal(err)
	}
	go tm.Dispatch()

	chainRouter := router.ChainRouter{}
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(t, err)
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &tm, time.Second, ids.Set{}, nil, router.HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	context := snow.DefaultConsensusContextTest()

	sender := Sender{}
	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(false)
	err = sender.Initialize(context, mc, externalSender, &chainRouter, &tm, 2, 2, 2)
	assert.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()
	handler, err := handler.New(
		mc,
		ctx,
		vdrs,
		nil,
		nil,
		time.Second,
	)
	assert.NoError(t, err)

	bootstrapper := &common.BootstrapperTest{
		BootstrapableTest: common.BootstrapableTest{
			T: t,
		},
		EngineTest: common.EngineTest{
			T: t,
		},
	}
	bootstrapper.Default(true)
	bootstrapper.CantGossip = false
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx }
	bootstrapper.ConnectedF = func(nodeID ids.ShortID, nodeVersion version.Application) error { return nil }
	queriesToSend := 2
	awaiting := make([]chan struct{}, queriesToSend)
	for i := 0; i < queriesToSend; i++ {
		awaiting[i] = make(chan struct{}, 1)
	}
	bootstrapper.QueryFailedF = func(nodeID ids.ShortID, reqID uint32) error {
		close(awaiting[int(reqID)])
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx.SetState(snow.Bootstrapping) // assumed bootstrap is ongoing

	chainRouter.AddChain(handler)
	handler.Start(false)

	go func() {
		for i := 0; i < queriesToSend; i++ {
			// Send a pull query to some random peer that won't respond
			// because they don't exist. This will almost immediately trigger
			// a query failed message
			vdrIDs := ids.ShortSet{}
			vdrIDs.Add(ids.GenerateTestShortID())
			sender.SendPullQuery(vdrIDs, uint32(i), ids.Empty)
		}
	}()

	for _, await := range awaiting {
		<-await
	}
}
