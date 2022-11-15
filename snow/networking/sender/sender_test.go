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
	require := require.New(t)
	vdrs := validators.NewSet()
	err := vdrs.AddWeight(ids.GenerateTestNodeID(), 1)
	require.NoError(err)
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
	require.NoError(err)
	go tm.Dispatch()

	chainRouter := router.ChainRouter{}

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(
		metrics,
		"dummyNamespace",
		true,
		10*time.Second,
	)
	require.NoError(err)

	err = chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Second,
		ids.Set{},
		ids.Set{},
		nil,
		router.HealthConfig{},
		"",
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	ctx := snow.DefaultConsensusContextTest()
	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(false)

	sender, err := New(
		ctx,
		mc,
		externalSender,
		&chainRouter,
		tm,
		defaultGossipConfig,
	)
	require.NoError(err)

	ctx2 := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)
	handler, err := handler.New(
		ctx2,
		vdrs,
		nil,
		nil,
		time.Hour,
		resourceTracker,
	)
	require.NoError(err)

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
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx
	}
	bootstrapper.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx2.SetState(snow.Bootstrapping) // assumed bootstrap is ongoing

	chainRouter.AddChain(context.Background(), handler)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	handler.Start(context.Background(), false)

	var (
		wg           = sync.WaitGroup{}
		vdrIDs       = ids.NodeIDSet{}
		chains       = ids.Set{}
		requestID    uint32
		failedLock   sync.Mutex
		failedVDRs   = ids.NodeIDSet{}
		failedChains = ids.Set{}
	)

	failed := func(_ context.Context, nodeID ids.NodeID, _ uint32) error {
		failedLock.Lock()
		defer failedLock.Unlock()

		failedVDRs.Add(nodeID)
		wg.Done()
		return nil
	}

	bootstrapper.GetStateSummaryFrontierFailedF = failed
	bootstrapper.GetAcceptedStateSummaryFailedF = failed
	bootstrapper.GetAcceptedFrontierFailedF = failed
	bootstrapper.GetAcceptedFailedF = failed
	bootstrapper.GetAncestorsFailedF = failed
	bootstrapper.GetFailedF = failed
	bootstrapper.QueryFailedF = failed
	bootstrapper.AppRequestFailedF = failed
	bootstrapper.CrossChainAppRequestFailedF = func(_ context.Context, chainID ids.ID, _ uint32) error {
		failedLock.Lock()
		defer failedLock.Unlock()

		failedChains.Add(chainID)
		wg.Done()
		return nil
	}

	sendAll := func() {
		{
			nodeIDs := ids.NodeIDSet{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetStateSummaryFrontier(context.Background(), nodeIDs, requestID)
		}
		{
			nodeIDs := ids.NodeIDSet{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetAcceptedStateSummary(context.Background(), nodeIDs, requestID, nil)
		}
		{
			nodeIDs := ids.NodeIDSet{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetAcceptedFrontier(context.Background(), nodeIDs, requestID)
		}
		{
			nodeIDs := ids.NodeIDSet{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetAccepted(context.Background(), nodeIDs, requestID, nil)
		}
		{
			nodeID := ids.GenerateTestNodeID()
			vdrIDs.Add(nodeID)
			wg.Add(1)
			requestID++
			sender.SendGetAncestors(context.Background(), nodeID, requestID, ids.Empty)
		}
		{
			nodeID := ids.GenerateTestNodeID()
			vdrIDs.Add(nodeID)
			wg.Add(1)
			requestID++
			sender.SendGet(context.Background(), nodeID, requestID, ids.Empty)
		}
		{
			nodeIDs := ids.NodeIDSet{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendPullQuery(context.Background(), nodeIDs, requestID, ids.Empty)
		}
		{
			nodeIDs := ids.NodeIDSet{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendPushQuery(context.Background(), nodeIDs, requestID, nil)
		}
		{
			nodeIDs := ids.NodeIDSet{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			err := sender.SendAppRequest(context.Background(), nodeIDs, requestID, nil)
			require.NoError(err)
		}
		{
			chainID := ids.GenerateTestID()
			chains.Add(chainID)
			wg.Add(1)
			requestID++
			err := sender.SendCrossChainAppRequest(context.Background(), chainID, requestID, nil)
			require.NoError(err)
		}
	}

	// Send messages to disconnected peers
	externalSender.SendF = func(_ message.OutboundMessage, nodeIDs ids.NodeIDSet, _ ids.ID, _ bool) ids.NodeIDSet {
		return nil
	}
	sendAll()

	// Send messages to connected peers
	externalSender.SendF = func(_ message.OutboundMessage, nodeIDs ids.NodeIDSet, _ ids.ID, _ bool) ids.NodeIDSet {
		return nodeIDs
	}
	sendAll()

	wg.Wait()

	require.Equal(vdrIDs, failedVDRs)
	require.Equal(chains, failedChains)
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
	mc, err := message.NewCreator(
		metrics,
		"dummyNamespace",
		true,
		10*time.Second,
	)
	require.NoError(t, err)

	err = chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Second,
		ids.Set{},
		ids.Set{},
		nil,
		router.HealthConfig{},
		"",
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()

	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(false)

	sender, err := New(
		ctx,
		mc,
		externalSender,
		&chainRouter,
		tm,
		defaultGossipConfig,
	)
	require.NoError(t, err)

	ctx2 := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handler, err := handler.New(
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
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx2
	}
	bootstrapper.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}
	queriesToSend := 1000
	awaiting := make([]chan struct{}, queriesToSend)
	for i := 0; i < queriesToSend; i++ {
		awaiting[i] = make(chan struct{}, 1)
	}
	bootstrapper.QueryFailedF = func(_ context.Context, _ ids.NodeID, reqID uint32) error {
		close(awaiting[int(reqID)])
		return nil
	}
	bootstrapper.CantGossip = false
	handler.SetBootstrapper(bootstrapper)
	ctx2.SetState(snow.Bootstrapping) // assumed bootstrap is ongoing

	chainRouter.AddChain(context.Background(), handler)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	handler.Start(context.Background(), false)

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
	mc, err := message.NewCreator(
		metrics,
		"dummyNamespace",
		true,
		10*time.Second,
	)
	require.NoError(t, err)

	err = chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Second,
		ids.Set{},
		ids.Set{},
		nil,
		router.HealthConfig{},
		"",
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	ctx := snow.DefaultConsensusContextTest()

	externalSender := &ExternalSenderTest{TB: t}
	externalSender.Default(false)

	sender, err := New(ctx, mc, externalSender, &chainRouter, tm, defaultGossipConfig)
	require.NoError(t, err)

	ctx2 := snow.DefaultConsensusContextTest()
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)
	handler, err := handler.New(
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
	bootstrapper.ContextF = func() *snow.ConsensusContext {
		return ctx2
	}
	bootstrapper.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		return nil
	}
	queriesToSend := 2
	awaiting := make([]chan struct{}, queriesToSend)
	for i := 0; i < queriesToSend; i++ {
		awaiting[i] = make(chan struct{}, 1)
	}
	bootstrapper.QueryFailedF = func(_ context.Context, _ ids.NodeID, reqID uint32) error {
		close(awaiting[int(reqID)])
		return nil
	}
	handler.SetBootstrapper(bootstrapper)
	ctx2.SetState(snow.Bootstrapping) // assumed bootstrap is ongoing

	chainRouter.AddChain(context.Background(), handler)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	handler.Start(context.Background(), false)

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
