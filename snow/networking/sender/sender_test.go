// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
)

var defaultSubnetConfig = subnets.Config{
	GossipConfig: subnets.GossipConfig{
		AcceptedFrontierPeerSize:  2,
		OnAcceptPeerSize:          2,
		AppGossipValidatorSize:    2,
		AppGossipNonValidatorSize: 2,
	},
}

func TestTimeout(t *testing.T) {
	require := require.New(t)
	vdrs := validators.NewSet()
	err := vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
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
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
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
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		subnets.New(ctx.NodeID, defaultSubnetConfig),
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
	h, err := handler.New(
		ctx2,
		vdrs,
		nil,
		time.Hour,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
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
	h.SetEngineManager(&handler.EngineManager{
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
	})
	ctx2.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping, // assumed bootstrap is ongoing
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(context.Background(), false)

	var (
		wg           = sync.WaitGroup{}
		vdrIDs       = set.Set[ids.NodeID]{}
		chains       = set.Set[ids.ID]{}
		requestID    uint32
		failedLock   sync.Mutex
		failedVDRs   = set.Set[ids.NodeID]{}
		failedChains = set.Set[ids.ID]{}
	)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	failed := func(ctx context.Context, nodeID ids.NodeID, _ uint32) error {
		require.NoError(ctx.Err())

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
	bootstrapper.CrossChainAppRequestFailedF = func(ctx context.Context, chainID ids.ID, _ uint32) error {
		require.NoError(ctx.Err())

		failedLock.Lock()
		defer failedLock.Unlock()

		failedChains.Add(chainID)
		wg.Done()
		return nil
	}

	sendAll := func() {
		{
			nodeIDs := set.Set[ids.NodeID]{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetStateSummaryFrontier(cancelledCtx, nodeIDs, requestID)
		}
		{
			nodeIDs := set.Set[ids.NodeID]{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetAcceptedStateSummary(cancelledCtx, nodeIDs, requestID, nil)
		}
		{
			nodeIDs := set.Set[ids.NodeID]{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetAcceptedFrontier(cancelledCtx, nodeIDs, requestID)
		}
		{
			nodeIDs := set.Set[ids.NodeID]{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetAccepted(cancelledCtx, nodeIDs, requestID, nil)
		}
		{
			nodeID := ids.GenerateTestNodeID()
			vdrIDs.Add(nodeID)
			wg.Add(1)
			requestID++
			sender.SendGetAncestors(cancelledCtx, nodeID, requestID, ids.Empty)
		}
		{
			nodeID := ids.GenerateTestNodeID()
			vdrIDs.Add(nodeID)
			wg.Add(1)
			requestID++
			sender.SendGet(cancelledCtx, nodeID, requestID, ids.Empty)
		}
		{
			nodeIDs := set.Set[ids.NodeID]{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendPullQuery(cancelledCtx, nodeIDs, requestID, ids.Empty)
		}
		{
			nodeIDs := set.Set[ids.NodeID]{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendPushQuery(cancelledCtx, nodeIDs, requestID, nil)
		}
		{
			nodeIDs := set.Set[ids.NodeID]{
				ids.GenerateTestNodeID(): struct{}{},
			}
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			err := sender.SendAppRequest(cancelledCtx, nodeIDs, requestID, nil)
			require.NoError(err)
		}
		{
			chainID := ids.GenerateTestID()
			chains.Add(chainID)
			wg.Add(1)
			requestID++
			err := sender.SendCrossChainAppRequest(cancelledCtx, chainID, requestID, nil)
			require.NoError(err)
		}
	}

	// Send messages to disconnected peers
	externalSender.SendF = func(_ message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		return nil
	}
	sendAll()

	// Send messages to connected peers
	externalSender.SendF = func(_ message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		return nodeIDs
	}
	sendAll()

	wg.Wait()

	require.Equal(vdrIDs, failedVDRs)
	require.Equal(chains, failedChains)
}

func TestReliableMessages(t *testing.T) {
	vdrs := validators.NewSet()
	err := vdrs.Add(ids.NodeID{1}, nil, ids.Empty, 1)
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
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
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
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		subnets.New(ctx.NodeID, defaultSubnetConfig),
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
	h, err := handler.New(
		ctx2,
		vdrs,
		nil,
		1,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
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
	h.SetEngineManager(&handler.EngineManager{
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
	})
	ctx2.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping, // assumed bootstrap is ongoing
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(context.Background(), false)

	go func() {
		for i := 0; i < queriesToSend; i++ {
			vdrIDs := set.Set[ids.NodeID]{}
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
	err := vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, 1)
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
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
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
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		subnets.New(ctx.NodeID, defaultSubnetConfig),
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
	h, err := handler.New(
		ctx2,
		vdrs,
		nil,
		time.Second,
		resourceTracker,
		validators.UnhandledSubnetConnector,
		subnets.New(ctx.NodeID, subnets.Config{}),
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
	h.SetEngineManager(&handler.EngineManager{
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
	})
	ctx2.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping, // assumed bootstrap is ongoing
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(context.Background(), false)

	go func() {
		for i := 0; i < queriesToSend; i++ {
			// Send a pull query to some random peer that won't respond
			// because they don't exist. This will almost immediately trigger
			// a query failed message
			vdrIDs := set.Set[ids.NodeID]{}
			vdrIDs.Add(ids.GenerateTestNodeID())
			sender.SendPullQuery(context.Background(), vdrIDs, uint32(i), ids.Empty)
		}
	}()

	for _, await := range awaiting {
		<-await
	}
}

func TestSender_Bootstrap_Requests(t *testing.T) {
	var (
		chainID       = ids.GenerateTestID()
		subnetID      = ids.GenerateTestID()
		myNodeID      = ids.GenerateTestNodeID()
		successNodeID = ids.GenerateTestNodeID()
		failedNodeID  = ids.GenerateTestNodeID()
		deadline      = time.Second
		requestID     = uint32(1337)
		ctx           = snow.DefaultContextTest()
		heights       = []uint64{1, 2, 3}
		containerIDs  = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
		engineType    = p2p.EngineType_ENGINE_TYPE_SNOWMAN
	)
	ctx.ChainID = chainID
	ctx.SubnetID = subnetID
	ctx.NodeID = myNodeID
	snowCtx := &snow.ConsensusContext{
		Context:             ctx,
		Registerer:          prometheus.NewRegistry(),
		AvalancheRegisterer: prometheus.NewRegistry(),
	}

	type test struct {
		name                    string
		failedMsgF              func(nodeID ids.NodeID) message.InboundMessage
		assertMsgToMyself       func(require *require.Assertions, msg message.InboundMessage)
		expectedResponseOp      message.Op
		setMsgCreatorExpect     func(msgCreator *message.MockOutboundMsgBuilder)
		setExternalSenderExpect func(externalSender *MockExternalSender)
		sendF                   func(require *require.Assertions, sender common.Sender, nodeIDs set.Set[ids.NodeID])
		engineType              p2p.EngineType
	}

	tests := []test{
		{
			name: "GetStateSummaryFrontier",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetStateSummaryFrontierFailed(
					nodeID,
					chainID,
					requestID,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*p2p.GetStateSummaryFrontier)
				require.True(ok)
				require.Equal(chainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(uint64(deadline), innerMsg.Deadline)
			},
			expectedResponseOp: message.StateSummaryFrontierOp,
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().GetStateSummaryFrontier(
					chainID,
					requestID,
					deadline,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{ // Note [myNodeID] is not in this set
						successNodeID: struct{}{},
						failedNodeID:  struct{}{},
					}, // Node IDs
					subnetID, // Subnet ID
					gomock.Any(),
				).Return(set.Set[ids.NodeID]{
					successNodeID: struct{}{},
				})
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeIDs set.Set[ids.NodeID]) {
				sender.SendGetStateSummaryFrontier(
					context.Background(),
					nodeIDs,
					requestID,
				)
			},
		},
		{
			name: "GetAcceptedStateSummary",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetAcceptedStateSummaryFailed(
					nodeID,
					chainID,
					requestID,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*p2p.GetAcceptedStateSummary)
				require.True(ok)
				require.Equal(chainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(uint64(deadline), innerMsg.Deadline)
				require.Equal(heights, innerMsg.Heights)
			},
			expectedResponseOp: message.AcceptedStateSummaryOp,
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().GetAcceptedStateSummary(
					chainID,
					requestID,
					deadline,
					heights,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{ // Note [myNodeID] is not in this set
						successNodeID: struct{}{},
						failedNodeID:  struct{}{},
					}, // Node IDs
					subnetID, // Subnet ID
					gomock.Any(),
				).Return(set.Set[ids.NodeID]{
					successNodeID: struct{}{},
				})
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeIDs set.Set[ids.NodeID]) {
				sender.SendGetAcceptedStateSummary(context.Background(), nodeIDs, requestID, heights)
			},
		},
		{
			name: "GetAcceptedFrontier",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetAcceptedFrontierFailed(
					nodeID,
					chainID,
					requestID,
					engineType,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*p2p.GetAcceptedFrontier)
				require.True(ok)
				require.Equal(chainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(uint64(deadline), innerMsg.Deadline)
				require.Equal(engineType, innerMsg.EngineType)
			},
			expectedResponseOp: message.AcceptedFrontierOp,
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().GetAcceptedFrontier(
					chainID,
					requestID,
					deadline,
					engineType,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{ // Note [myNodeID] is not in this set
						successNodeID: struct{}{},
						failedNodeID:  struct{}{},
					}, // Node IDs
					subnetID, // Subnet ID
					gomock.Any(),
				).Return(set.Set[ids.NodeID]{
					successNodeID: struct{}{},
				})
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeIDs set.Set[ids.NodeID]) {
				sender.SendGetAcceptedFrontier(context.Background(), nodeIDs, requestID)
			},
			engineType: engineType,
		},
		{
			name: "GetAccepted",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetAcceptedFailed(
					nodeID,
					chainID,
					requestID,
					engineType,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*p2p.GetAccepted)
				require.True(ok)
				require.Equal(chainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(uint64(deadline), innerMsg.Deadline)
				require.Equal(engineType, innerMsg.EngineType)
			},
			expectedResponseOp: message.AcceptedOp,
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().GetAccepted(
					chainID,
					requestID,
					deadline,
					containerIDs,
					engineType,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{ // Note [myNodeID] is not in this set
						successNodeID: struct{}{},
						failedNodeID:  struct{}{},
					}, // Node IDs
					subnetID, // Subnet ID
					gomock.Any(),
				).Return(set.Set[ids.NodeID]{
					successNodeID: struct{}{},
				})
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeIDs set.Set[ids.NodeID]) {
				sender.SendGetAccepted(context.Background(), nodeIDs, requestID, containerIDs)
			},
			engineType: engineType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var (
				msgCreator     = message.NewMockOutboundMsgBuilder(ctrl)
				externalSender = NewMockExternalSender(ctrl)
				timeoutManager = timeout.NewMockManager(ctrl)
				router         = router.NewMockRouter(ctrl)
				nodeIDs        = set.Set[ids.NodeID]{
					successNodeID: struct{}{},
					failedNodeID:  struct{}{},
					myNodeID:      struct{}{},
				}
				nodeIDsCopy set.Set[ids.NodeID]
			)
			nodeIDsCopy.Union(nodeIDs)
			snowCtx.Registerer = prometheus.NewRegistry()

			sender, err := New(
				snowCtx,
				msgCreator,
				externalSender,
				router,
				timeoutManager,
				engineType,
				subnets.New(ctx.NodeID, defaultSubnetConfig),
			)
			require.NoError(err)

			// Set the timeout (deadline)
			timeoutManager.EXPECT().TimeoutDuration().Return(deadline).AnyTimes()

			// Make sure we register requests with the router
			for nodeID := range nodeIDs {
				expectedFailedMsg := tt.failedMsgF(nodeID)
				router.EXPECT().RegisterRequest(
					gomock.Any(),          // Context
					nodeID,                // Node ID
					chainID,               // Source Chain
					chainID,               // Destination Chain
					requestID,             // Request ID
					tt.expectedResponseOp, // Operation
					expectedFailedMsg,     // Failure Message
					tt.engineType,
				)
			}

			// Make sure we send a message to ourselves since [myNodeID]
			// is in [nodeIDs].
			// Note that HandleInbound is called in a separate goroutine
			// so we need to use a channel to synchronize the test.
			calledHandleInbound := make(chan struct{})
			router.EXPECT().HandleInbound(gomock.Any(), gomock.Any()).Do(
				func(_ context.Context, msg message.InboundMessage) {
					// Make sure we're sending ourselves
					// the expected message.
					tt.assertMsgToMyself(require, msg)
					close(calledHandleInbound)
				},
			)

			// Make sure we're making the correct outbound message.
			tt.setMsgCreatorExpect(msgCreator)

			// Make sure we're sending the message
			tt.setExternalSenderExpect(externalSender)

			tt.sendF(require, sender, nodeIDsCopy)

			<-calledHandleInbound
		})
	}
}

func TestSender_Bootstrap_Responses(t *testing.T) {
	var (
		chainID           = ids.GenerateTestID()
		subnetID          = ids.GenerateTestID()
		myNodeID          = ids.GenerateTestNodeID()
		destinationNodeID = ids.GenerateTestNodeID()
		deadline          = time.Second
		requestID         = uint32(1337)
		ctx               = snow.DefaultContextTest()
		summaryIDs        = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
		summary           = []byte{1, 2, 3}
		engineType        = p2p.EngineType_ENGINE_TYPE_AVALANCHE
	)
	ctx.ChainID = chainID
	ctx.SubnetID = subnetID
	ctx.NodeID = myNodeID
	snowCtx := &snow.ConsensusContext{
		Context:             ctx,
		Registerer:          prometheus.NewRegistry(),
		AvalancheRegisterer: prometheus.NewRegistry(),
	}

	type test struct {
		name                    string
		assertMsgToMyself       func(require *require.Assertions, msg message.InboundMessage)
		setMsgCreatorExpect     func(msgCreator *message.MockOutboundMsgBuilder)
		setExternalSenderExpect func(externalSender *MockExternalSender)
		sendF                   func(require *require.Assertions, sender common.Sender, nodeID ids.NodeID)
	}

	tests := []test{
		{
			name: "StateSummaryFrontier",
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().StateSummaryFrontier(
					chainID,
					requestID,
					summary,
				).Return(nil, nil) // Don't care about the message
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*p2p.StateSummaryFrontier)
				require.True(ok)
				require.Equal(chainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(summary, innerMsg.Summary)
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{destinationNodeID: struct{}{}}, // Node IDs
					subnetID, // Subnet ID
					gomock.Any(),
				).Return(nil)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendStateSummaryFrontier(context.Background(), nodeID, requestID, summary)
			},
		},
		{
			name: "AcceptedStateSummary",
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().AcceptedStateSummary(
					chainID,
					requestID,
					summaryIDs,
				).Return(nil, nil) // Don't care about the message
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*p2p.AcceptedStateSummary)
				require.True(ok)
				require.Equal(chainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				for i, summaryID := range summaryIDs {
					require.Equal(summaryID[:], innerMsg.SummaryIds[i])
				}
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{destinationNodeID: struct{}{}}, // Node IDs
					subnetID, // Subnet ID
					gomock.Any(),
				).Return(nil)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendAcceptedStateSummary(context.Background(), nodeID, requestID, summaryIDs)
			},
		},
		{
			name: "AcceptedFrontier",
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().AcceptedFrontier(
					chainID,
					requestID,
					summaryIDs,
				).Return(nil, nil) // Don't care about the message
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*p2p.AcceptedFrontier)
				require.True(ok)
				require.Equal(chainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				for i, summaryID := range summaryIDs {
					require.Equal(summaryID[:], innerMsg.ContainerIds[i])
				}
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{destinationNodeID: struct{}{}}, // Node IDs
					subnetID, // Subnet ID
					gomock.Any(),
				).Return(nil)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendAcceptedFrontier(context.Background(), nodeID, requestID, summaryIDs)
			},
		},
		{
			name: "Accepted",
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().Accepted(
					chainID,
					requestID,
					summaryIDs,
				).Return(nil, nil) // Don't care about the message
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*p2p.Accepted)
				require.True(ok)
				require.Equal(chainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				for i, summaryID := range summaryIDs {
					require.Equal(summaryID[:], innerMsg.ContainerIds[i])
				}
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{destinationNodeID: struct{}{}}, // Node IDs
					subnetID, // Subnet ID
					gomock.Any(),
				).Return(nil)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendAccepted(context.Background(), nodeID, requestID, summaryIDs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var (
				msgCreator     = message.NewMockOutboundMsgBuilder(ctrl)
				externalSender = NewMockExternalSender(ctrl)
				timeoutManager = timeout.NewMockManager(ctrl)
				router         = router.NewMockRouter(ctrl)
			)

			// Instantiate new registerers to avoid duplicate metrics
			// registration
			snowCtx.Registerer = prometheus.NewRegistry()
			snowCtx.AvalancheRegisterer = prometheus.NewRegistry()

			sender, err := New(
				snowCtx,
				msgCreator,
				externalSender,
				router,
				timeoutManager,
				engineType,
				subnets.New(ctx.NodeID, defaultSubnetConfig),
			)
			require.NoError(err)

			// Set the timeout (deadline)
			timeoutManager.EXPECT().TimeoutDuration().Return(deadline).AnyTimes()

			// Case: sending to ourselves
			{
				calledHandleInbound := make(chan struct{})
				router.EXPECT().HandleInbound(gomock.Any(), gomock.Any()).Do(
					func(_ context.Context, msg message.InboundMessage) {
						// Make sure we're sending ourselves
						// the expected message.
						tt.assertMsgToMyself(require, msg)
						close(calledHandleInbound)
					},
				)
				tt.sendF(require, sender, myNodeID)
				<-calledHandleInbound
			}

			// Case: not sending to ourselves

			// Make sure we're making the correct outbound message.
			tt.setMsgCreatorExpect(msgCreator)

			// Make sure we're sending the message
			tt.setExternalSenderExpect(externalSender)

			tt.sendF(require, sender, destinationNodeID)
		})
	}
}

func TestSender_Single_Request(t *testing.T) {
	var (
		chainID           = ids.GenerateTestID()
		subnetID          = ids.GenerateTestID()
		myNodeID          = ids.GenerateTestNodeID()
		destinationNodeID = ids.GenerateTestNodeID()
		deadline          = time.Second
		requestID         = uint32(1337)
		ctx               = snow.DefaultContextTest()
		containerID       = ids.GenerateTestID()
		engineType        = p2p.EngineType_ENGINE_TYPE_SNOWMAN
	)
	ctx.ChainID = chainID
	ctx.SubnetID = subnetID
	ctx.NodeID = myNodeID
	snowCtx := &snow.ConsensusContext{
		Context:             ctx,
		Registerer:          prometheus.NewRegistry(),
		AvalancheRegisterer: prometheus.NewRegistry(),
	}

	type test struct {
		name                    string
		failedMsgF              func(nodeID ids.NodeID) message.InboundMessage
		assertMsgToMyself       func(require *require.Assertions, msg message.InboundMessage)
		expectedResponseOp      message.Op
		setMsgCreatorExpect     func(msgCreator *message.MockOutboundMsgBuilder)
		setExternalSenderExpect func(externalSender *MockExternalSender, sentTo set.Set[ids.NodeID])
		sendF                   func(require *require.Assertions, sender common.Sender, nodeID ids.NodeID)
	}

	tests := []test{
		{
			name: "GetAncestors",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetAncestorsFailed(
					nodeID,
					chainID,
					requestID,
					engineType,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*message.GetAncestorsFailed)
				require.True(ok)
				require.Equal(chainID, innerMsg.ChainID)
				require.Equal(requestID, innerMsg.RequestID)
				require.Equal(engineType, innerMsg.EngineType)
			},
			expectedResponseOp: message.AncestorsOp,
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().GetAncestors(
					chainID,
					requestID,
					deadline,
					containerID,
					engineType,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender, sentTo set.Set[ids.NodeID]) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{destinationNodeID: struct{}{}}, // Node IDs
					subnetID,
					gomock.Any(),
				).Return(sentTo)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendGetAncestors(context.Background(), nodeID, requestID, containerID)
			},
		},
		{
			name: "Get",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetFailed(
					nodeID,
					chainID,
					requestID,
					engineType,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				innerMsg, ok := msg.Message().(*message.GetFailed)
				require.True(ok)
				require.Equal(chainID, innerMsg.ChainID)
				require.Equal(requestID, innerMsg.RequestID)
				require.Equal(engineType, innerMsg.EngineType)
			},
			expectedResponseOp: message.PutOp,
			setMsgCreatorExpect: func(msgCreator *message.MockOutboundMsgBuilder) {
				msgCreator.EXPECT().Get(
					chainID,
					requestID,
					deadline,
					containerID,
					engineType,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *MockExternalSender, sentTo set.Set[ids.NodeID]) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					set.Set[ids.NodeID]{destinationNodeID: struct{}{}}, // Node IDs
					subnetID,
					gomock.Any(),
				).Return(sentTo)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendGet(context.Background(), nodeID, requestID, containerID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var (
				msgCreator     = message.NewMockOutboundMsgBuilder(ctrl)
				externalSender = NewMockExternalSender(ctrl)
				timeoutManager = timeout.NewMockManager(ctrl)
				router         = router.NewMockRouter(ctrl)
			)
			snowCtx.Registerer = prometheus.NewRegistry()

			sender, err := New(
				snowCtx,
				msgCreator,
				externalSender,
				router,
				timeoutManager,
				engineType,
				subnets.New(ctx.NodeID, defaultSubnetConfig),
			)
			require.NoError(err)

			// Set the timeout (deadline)
			timeoutManager.EXPECT().TimeoutDuration().Return(deadline).AnyTimes()

			// Case: sending to myself
			{
				// Make sure we register requests with the router
				expectedFailedMsg := tt.failedMsgF(myNodeID)
				router.EXPECT().RegisterRequest(
					gomock.Any(),          // Context
					myNodeID,              // Node ID
					chainID,               // Source Chain
					chainID,               // Destination Chain
					requestID,             // Request ID
					tt.expectedResponseOp, // Operation
					expectedFailedMsg,     // Failure Message
					engineType,            // Engine Type
				)

				// Note that HandleInbound is called in a separate goroutine
				// so we need to use a channel to synchronize the test.
				calledHandleInbound := make(chan struct{})
				router.EXPECT().HandleInbound(gomock.Any(), gomock.Any()).Do(
					func(_ context.Context, msg message.InboundMessage) {
						// Make sure we're sending ourselves
						// the expected message.
						tt.assertMsgToMyself(require, msg)
						close(calledHandleInbound)
					},
				)

				tt.sendF(require, sender, myNodeID)

				<-calledHandleInbound
			}

			// Case: Node is benched
			{
				timeoutManager.EXPECT().IsBenched(destinationNodeID, chainID).Return(true)

				timeoutManager.EXPECT().RegisterRequestToUnreachableValidator()

				// Make sure we register requests with the router
				expectedFailedMsg := tt.failedMsgF(destinationNodeID)
				router.EXPECT().RegisterRequest(
					gomock.Any(),          // Context
					destinationNodeID,     // Node ID
					chainID,               // Source Chain
					chainID,               // Destination Chain
					requestID,             // Request ID
					tt.expectedResponseOp, // Operation
					expectedFailedMsg,     // Failure Message
					engineType,            // Engine Type
				)

				// Note that HandleInbound is called in a separate goroutine
				// so we need to use a channel to synchronize the test.
				calledHandleInbound := make(chan struct{})
				router.EXPECT().HandleInbound(gomock.Any(), gomock.Any()).Do(
					func(_ context.Context, msg message.InboundMessage) {
						// Make sure we're sending ourselves
						// the expected message.
						tt.assertMsgToMyself(require, msg)
						close(calledHandleInbound)
					},
				)

				tt.sendF(require, sender, destinationNodeID)

				<-calledHandleInbound
			}

			// Case: Node is not myself, not benched and send fails
			{
				timeoutManager.EXPECT().IsBenched(destinationNodeID, chainID).Return(false)

				timeoutManager.EXPECT().RegisterRequestToUnreachableValidator()

				// Make sure we register requests with the router
				expectedFailedMsg := tt.failedMsgF(destinationNodeID)
				router.EXPECT().RegisterRequest(
					gomock.Any(),          // Context
					destinationNodeID,     // Node ID
					chainID,               // Source Chain
					chainID,               // Destination Chain
					requestID,             // Request ID
					tt.expectedResponseOp, // Operation
					expectedFailedMsg,     // Failure Message
					engineType,            // Engine Type
				)

				// Note that HandleInbound is called in a separate goroutine
				// so we need to use a channel to synchronize the test.
				calledHandleInbound := make(chan struct{})
				router.EXPECT().HandleInbound(gomock.Any(), gomock.Any()).Do(
					func(_ context.Context, msg message.InboundMessage) {
						// Make sure we're sending ourselves
						// the expected message.
						tt.assertMsgToMyself(require, msg)
						close(calledHandleInbound)
					},
				)

				// Make sure we're making the correct outbound message.
				tt.setMsgCreatorExpect(msgCreator)

				// Make sure we're sending the message
				tt.setExternalSenderExpect(externalSender, set.Set[ids.NodeID]{})

				tt.sendF(require, sender, destinationNodeID)

				<-calledHandleInbound
			}
		})
	}
}
