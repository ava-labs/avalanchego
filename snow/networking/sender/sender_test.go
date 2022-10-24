// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
)

var defaultGossipConfig = GossipConfig{
	AcceptedFrontierPeerSize:  2,
	OnAcceptPeerSize:          2,
	AppGossipValidatorSize:    2,
	AppGossipNonValidatorSize: 2,
}

func TestTimeout(t *testing.T) {
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestNodeID(), 1)
	require.NoError(t, err)
	benchlist := benchlist.NewNoBenchlist()
	tm, err := timeout.NewManager(
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
	require.NoError(t, err)
	go tm.Dispatch()

	chainRouter := router.ChainRouter{}

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, "dummyNamespace", true, 10*time.Second)
	require.NoError(t, err)

	err = chainRouter.Initialize(ids.EmptyNodeID, logging.NoLog{}, mc, tm, time.Second, ids.Set{}, ids.Set{}, nil, router.HealthConfig{}, "", prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()
	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(false)

	sender, err := New(ctx, mc, externalSender, &chainRouter, tm, defaultGossipConfig)
	require.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(2)
	failedVDRs := ids.NodeIDSet{}
	ctx2 := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(t, err)
	handler, err := handler.New(
		mc,
		ctx2,
		vdrs,
		nil,
		nil,
		time.Hour,
		resourceTracker,
	)
	require.NoError(t, err)

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
	bootstrapper.ConnectedF = func(nodeID ids.NodeID, nodeVersion *version.Application) error { return nil }
	bootstrapper.QueryFailedF = func(ctx context.Context, nodeID ids.NodeID, _ uint32) error {
		failedVDRs.Add(nodeID)
		wg.Done()
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx2.SetState(snow.Bootstrapping) // assumed bootstrap is ongoing

	chainRouter.AddChain(handler)

	bootstrapper.StartF = func(startReqID uint32) error { return nil }
	handler.Start(false)

	vdrIDs := ids.NodeIDSet{}
	vdrIDs.Add(ids.NodeID{255})
	vdrIDs.Add(ids.NodeID{254})

	sender.SendPullQuery(context.Background(), vdrIDs, 0, ids.Empty)

	wg.Wait()

	if !failedVDRs.Equals(vdrIDs) {
		t.Fatalf("Timeouts should have fired")
	}
}

func TestReliableMessages(t *testing.T) {
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.NodeID{1}, 1)
	require.NoError(t, err)
	benchlist := benchlist.NewNoBenchlist()
	tm, err := timeout.NewManager(
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
	require.NoError(t, err)

	go tm.Dispatch()

	chainRouter := router.ChainRouter{}

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, "dummyNamespace", true, 10*time.Second)
	require.NoError(t, err)

	err = chainRouter.Initialize(ids.EmptyNodeID, logging.NoLog{}, mc, tm, time.Second, ids.Set{}, ids.Set{}, nil, router.HealthConfig{}, "", prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()

	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(false)

	sender, err := New(ctx, mc, externalSender, &chainRouter, tm, defaultGossipConfig)
	require.NoError(t, err)

	ctx2 := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(t, err)
	handler, err := handler.New(
		mc,
		ctx2,
		vdrs,
		nil,
		nil,
		1,
		resourceTracker,
	)
	require.NoError(t, err)

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
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx2 }
	bootstrapper.ConnectedF = func(nodeID ids.NodeID, nodeVersion *version.Application) error { return nil }
	queriesToSend := 1000
	awaiting := make([]chan struct{}, queriesToSend)
	for i := 0; i < queriesToSend; i++ {
		awaiting[i] = make(chan struct{}, 1)
	}
	bootstrapper.QueryFailedF = func(ctx context.Context, nodeID ids.NodeID, reqID uint32) error {
		close(awaiting[int(reqID)])
		return nil
	}
	bootstrapper.CantGossip = false
	handler.SetBootstrapper(bootstrapper)
	ctx2.SetState(snow.Bootstrapping) // assumed bootstrap is ongoing

	chainRouter.AddChain(handler)

	bootstrapper.StartF = func(startReqID uint32) error { return nil }
	handler.Start(false)

	go func() {
		for i := 0; i < queriesToSend; i++ {
			vdrIDs := ids.NodeIDSet{}
			vdrIDs.Add(ids.NodeID{1})

			sender.SendPullQuery(context.Background(), vdrIDs, uint32(i), ids.Empty)
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
	err := vdrs.AddWeight(ids.GenerateTestNodeID(), 1)
	require.NoError(t, err)
	tm, err := timeout.NewManager(
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
	require.NoError(t, err)

	go tm.Dispatch()

	chainRouter := router.ChainRouter{}

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, "dummyNamespace", true, 10*time.Second)
	require.NoError(t, err)

	err = chainRouter.Initialize(ids.EmptyNodeID, logging.NoLog{}, mc, tm, time.Second, ids.Set{}, ids.Set{}, nil, router.HealthConfig{}, "", prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()

	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(false)

	sender, err := New(ctx, mc, externalSender, &chainRouter, tm, defaultGossipConfig)
	require.NoError(t, err)

	ctx2 := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(t, err)
	handler, err := handler.New(
		mc,
		ctx2,
		vdrs,
		nil,
		nil,
		time.Second,
		resourceTracker,
	)
	require.NoError(t, err)

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
	bootstrapper.ContextF = func() *snow.ConsensusContext { return ctx2 }
	bootstrapper.ConnectedF = func(nodeID ids.NodeID, nodeVersion *version.Application) error { return nil }
	queriesToSend := 2
	awaiting := make([]chan struct{}, queriesToSend)
	for i := 0; i < queriesToSend; i++ {
		awaiting[i] = make(chan struct{}, 1)
	}
	bootstrapper.QueryFailedF = func(ctx context.Context, nodeID ids.NodeID, reqID uint32) error {
		close(awaiting[int(reqID)])
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx2.SetState(snow.Bootstrapping) // assumed bootstrap is ongoing

	chainRouter.AddChain(handler)

	bootstrapper.StartF = func(startReqID uint32) error { return nil }
	handler.Start(false)

	go func() {
		for i := 0; i < queriesToSend; i++ {
			// Send a pull query to some random peer that won't respond
			// because they don't exist. This will almost immediately trigger
			// a query failed message
			vdrIDs := ids.NodeIDSet{}
			vdrIDs.Add(ids.GenerateTestNodeID())
			sender.SendPullQuery(context.Background(), vdrIDs, uint32(i), ids.Empty)
		}
	}()

	for _, await := range awaiting {
		<-await
	}
}
