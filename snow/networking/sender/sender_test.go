// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender_test

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/message/messagemock"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/router/routermock"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendermock"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendertest"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/timeout/timeoutmock"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	commontracker "github.com/ava-labs/avalanchego/snow/engine/common/tracker"

	. "github.com/ava-labs/avalanchego/snow/networking/sender"
)

const testThreadPoolSize = 2

func TestTimeout(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))
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
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	go tm.Dispatch()

	chainRouter := router.ChainRouter{}

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(
		metrics,
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(err)

	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Second,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		router.HealthConfig{},
		prometheus.NewRegistry(),
	))

	externalSender := &sendertest.External{TB: t}
	externalSender.Default(false)

	sender, err := New(
		ctx,
		mc,
		externalSender,
		&chainRouter,
		tm,
		p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		subnets.New(ctx.NodeID, subnets.Config{}),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	ctx2 := snowtest.ConsensusContext(snowCtx)
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	p2pTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	h, err := handler.New(
		ctx2,
		&block.ChangeNotifier{},
		noopSubscription,
		vdrs,
		time.Hour,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		p2pTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
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
		DAG: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
	})
	ctx2.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
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
	bootstrapper.AppRequestFailedF = func(ctx context.Context, nodeID ids.NodeID, _ uint32, _ *common.AppError) error {
		require.NoError(ctx.Err())

		failedLock.Lock()
		defer failedLock.Unlock()

		failedVDRs.Add(nodeID)
		wg.Done()
		return nil
	}

	sendAll := func() {
		{
			nodeIDs := set.Of(ids.GenerateTestNodeID())
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetStateSummaryFrontier(cancelledCtx, nodeIDs, requestID)
		}
		{
			nodeIDs := set.Of(ids.GenerateTestNodeID())
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetAcceptedStateSummary(cancelledCtx, nodeIDs, requestID, nil)
		}
		{
			nodeIDs := set.Of(ids.GenerateTestNodeID())
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendGetAcceptedFrontier(cancelledCtx, nodeIDs, requestID)
		}
		{
			nodeIDs := set.Of(ids.GenerateTestNodeID())
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
			nodeIDs := set.Of(ids.GenerateTestNodeID())
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendPullQuery(cancelledCtx, nodeIDs, requestID, ids.Empty, 0)
		}
		{
			nodeIDs := set.Of(ids.GenerateTestNodeID())
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			sender.SendPushQuery(cancelledCtx, nodeIDs, requestID, nil, 0)
		}
		{
			nodeIDs := set.Of(ids.GenerateTestNodeID())
			vdrIDs.Union(nodeIDs)
			wg.Add(1)
			requestID++
			require.NoError(sender.SendAppRequest(cancelledCtx, nodeIDs, requestID, nil))
		}
	}

	// Send messages to disconnected peers
	externalSender.SendF = func(message.OutboundMessage, common.SendConfig, ids.ID, subnets.Allower) set.Set[ids.NodeID] {
		return nil
	}
	sendAll()

	// Send messages to connected peers
	externalSender.SendF = func(_ message.OutboundMessage, config common.SendConfig, _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		return config.NodeIDs
	}
	sendAll()

	wg.Wait()

	require.Equal(vdrIDs, failedVDRs)
	require.Equal(chains, failedChains)
}

func TestReliableMessages(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.BuildTestNodeID([]byte{1}), nil, ids.Empty, 1))
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
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go tm.Dispatch()

	chainRouter := router.ChainRouter{}

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(
		metrics,
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(err)

	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Second,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		router.HealthConfig{},
		prometheus.NewRegistry(),
	))

	externalSender := &sendertest.External{TB: t}
	externalSender.Default(false)

	sender, err := New(
		ctx,
		mc,
		externalSender,
		&chainRouter,
		tm,
		p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		subnets.New(ctx.NodeID, subnets.Config{}),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	ctx2 := snowtest.ConsensusContext(snowCtx)
	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	p2pTracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	h, err := handler.New(
		ctx2,
		&block.ChangeNotifier{},
		noopSubscription,
		vdrs,
		1,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		p2pTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
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
		DAG: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    nil,
		},
	})
	ctx2.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.Bootstrapping, // assumed bootstrap is ongoing
	})

	chainRouter.AddChain(context.Background(), h)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}
	h.Start(context.Background(), false)

	go func() {
		for i := 0; i < queriesToSend; i++ {
			vdrIDs := set.Of(ids.BuildTestNodeID([]byte{1}))

			sender.SendPullQuery(context.Background(), vdrIDs, uint32(i), ids.Empty, 0)
			time.Sleep(time.Duration(rand.Float64() * float64(time.Microsecond))) // #nosec G404
		}
	}()

	for _, await := range awaiting {
		<-await
	}
}

func TestReliableMessagesToMyself(t *testing.T) {
	for _, validatorOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("validatorOnly_%v", validatorOnly), func(t *testing.T) {
			require := require.New(t)

			benchlist := benchlist.NewNoBenchlist()
			snowCtx := snowtest.Context(t, snowtest.CChainID)
			ctx := snowtest.ConsensusContext(snowCtx)
			vdrs := validators.NewManager()
			require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))
			tm, err := timeout.NewManager(
				&timer.AdaptiveTimeoutConfig{
					InitialTimeout:     10 * time.Millisecond,
					MinimumTimeout:     10 * time.Millisecond,
					MaximumTimeout:     10 * time.Millisecond, // Timeout fires immediately
					TimeoutHalflife:    5 * time.Minute,
					TimeoutCoefficient: 1.25,
				},
				benchlist,
				prometheus.NewRegistry(),
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			go tm.Dispatch()

			chainRouter := router.ChainRouter{}

			metrics := prometheus.NewRegistry()
			mc, err := message.NewCreator(
				metrics,
				constants.DefaultNetworkCompressionType,
				10*time.Second,
			)
			require.NoError(err)

			require.NoError(chainRouter.Initialize(
				ids.EmptyNodeID,
				logging.NoLog{},
				tm,
				time.Second,
				set.Set[ids.ID]{},
				true,
				set.Set[ids.ID]{},
				nil,
				router.HealthConfig{},
				prometheus.NewRegistry(),
			))

			externalSender := &sendertest.External{TB: t}
			externalSender.Default(false)

			subnet := subnets.New(ctx.NodeID, subnets.Config{
				ValidatorOnly: validatorOnly,
			})
			sender, err := New(
				ctx,
				mc,
				externalSender,
				&chainRouter,
				tm,
				p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				subnet,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			ctx2 := snowtest.ConsensusContext(snowCtx)
			resourceTracker, err := tracker.NewResourceTracker(
				prometheus.NewRegistry(),
				resource.NoUsage,
				meter.ContinuousFactory{},
				time.Second,
			)
			require.NoError(err)

			p2pTracker, err := p2p.NewPeerTracker(
				logging.NoLog{},
				"",
				prometheus.NewRegistry(),
				nil,
				version.CurrentApp,
			)
			require.NoError(err)

			h, err := handler.New(
				ctx2,
				&block.ChangeNotifier{},
				noopSubscription,
				vdrs,
				time.Second,
				testThreadPoolSize,
				resourceTracker,
				subnet,
				commontracker.NewPeers(),
				p2pTracker,
				prometheus.NewRegistry(),
				func() {},
			)
			require.NoError(err)

			bootstrapper := &enginetest.Bootstrapper{
				Engine: enginetest.Engine{
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
				DAG: &handler.Engine{
					StateSyncer:  nil,
					Bootstrapper: bootstrapper,
					Consensus:    nil,
				},
				Chain: &handler.Engine{
					StateSyncer:  nil,
					Bootstrapper: bootstrapper,
					Consensus:    nil,
				},
			})
			ctx2.State.Set(snow.EngineState{
				Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
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
					vdrIDs := set.Of(ids.GenerateTestNodeID())
					sender.SendPullQuery(context.Background(), vdrIDs, uint32(i), ids.Empty, 0)
				}
			}()

			for _, await := range awaiting {
				<-await
			}
		})
	}
}

func TestSender_Bootstrap_Requests(t *testing.T) {
	var (
		successNodeID = ids.GenerateTestNodeID()
		failedNodeID  = ids.GenerateTestNodeID()
		deadline      = time.Second
		requestID     = uint32(1337)
		heights       = []uint64{1, 2, 3}
		containerIDs  = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
	)
	snowCtx := snowtest.Context(t, snowtest.PChainID)
	ctx := snowtest.ConsensusContext(snowCtx)

	type test struct {
		name                    string
		failedMsgF              func(nodeID ids.NodeID) message.InboundMessage
		assertMsgToMyself       func(require *require.Assertions, msg message.InboundMessage)
		expectedResponseOp      message.Op
		setMsgCreatorExpect     func(msgCreator *messagemock.OutboundMsgBuilder)
		setExternalSenderExpect func(externalSender *sendermock.ExternalSender)
		sendF                   func(require *require.Assertions, sender common.Sender, nodeIDs set.Set[ids.NodeID])
	}

	tests := []test{
		{
			name: "GetStateSummaryFrontier",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetStateSummaryFrontierFailed(
					nodeID,
					ctx.ChainID,
					requestID,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&p2ppb.GetStateSummaryFrontier{}, msg.Message())
				innerMsg := msg.Message().(*p2ppb.GetStateSummaryFrontier)
				require.Equal(ctx.ChainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(uint64(deadline), innerMsg.Deadline)
			},
			expectedResponseOp: message.StateSummaryFrontierOp,
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().GetStateSummaryFrontier(
					ctx.ChainID,
					requestID,
					deadline,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						// Note [myNodeID] is not in this set
						NodeIDs: set.Of(successNodeID, failedNodeID),
					},
					ctx.SubnetID, // Subnet ID
					gomock.Any(),
				).Return(set.Of(successNodeID))
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
					ctx.ChainID,
					requestID,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&p2ppb.GetAcceptedStateSummary{}, msg.Message())
				innerMsg := msg.Message().(*p2ppb.GetAcceptedStateSummary)
				require.Equal(ctx.ChainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(uint64(deadline), innerMsg.Deadline)
				require.Equal(heights, innerMsg.Heights)
			},
			expectedResponseOp: message.AcceptedStateSummaryOp,
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().GetAcceptedStateSummary(
					ctx.ChainID,
					requestID,
					deadline,
					heights,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						// Note [myNodeID] is not in this set
						NodeIDs: set.Of(successNodeID, failedNodeID),
					},
					ctx.SubnetID, // Subnet ID
					gomock.Any(),
				).Return(set.Of(successNodeID))
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
					ctx.ChainID,
					requestID,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&p2ppb.GetAcceptedFrontier{}, msg.Message())
				innerMsg := msg.Message().(*p2ppb.GetAcceptedFrontier)
				require.Equal(ctx.ChainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(uint64(deadline), innerMsg.Deadline)
			},
			expectedResponseOp: message.AcceptedFrontierOp,
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().GetAcceptedFrontier(
					ctx.ChainID,
					requestID,
					deadline,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						// Note [myNodeID] is not in this set
						NodeIDs: set.Of(successNodeID, failedNodeID),
					},
					ctx.SubnetID, // Subnet ID
					gomock.Any(),
				).Return(set.Of(successNodeID))
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeIDs set.Set[ids.NodeID]) {
				sender.SendGetAcceptedFrontier(context.Background(), nodeIDs, requestID)
			},
		},
		{
			name: "GetAccepted",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetAcceptedFailed(
					nodeID,
					ctx.ChainID,
					requestID,
				)
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&p2ppb.GetAccepted{}, msg.Message())
				innerMsg := msg.Message().(*p2ppb.GetAccepted)
				require.Equal(ctx.ChainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(uint64(deadline), innerMsg.Deadline)
			},
			expectedResponseOp: message.AcceptedOp,
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().GetAccepted(
					ctx.ChainID,
					requestID,
					deadline,
					containerIDs,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						// Note [myNodeID] is not in this set
						NodeIDs: set.Of(successNodeID, failedNodeID),
					},
					ctx.SubnetID, // Subnet ID
					gomock.Any(),
				).Return(set.Of(successNodeID))
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeIDs set.Set[ids.NodeID]) {
				sender.SendGetAccepted(context.Background(), nodeIDs, requestID, containerIDs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			var (
				msgCreator     = messagemock.NewOutboundMsgBuilder(ctrl)
				externalSender = sendermock.NewExternalSender(ctrl)
				timeoutManager = timeoutmock.NewManager(ctrl)
				router         = routermock.NewRouter(ctrl)
				nodeIDs        = set.Of(successNodeID, failedNodeID, ctx.NodeID)
				nodeIDsCopy    set.Set[ids.NodeID]
			)
			nodeIDsCopy.Union(nodeIDs)

			// Instantiate new registerers to avoid duplicate metrics
			// registration
			ctx.Registerer = prometheus.NewRegistry()

			sender, err := New(
				ctx,
				msgCreator,
				externalSender,
				router,
				timeoutManager,
				p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				subnets.New(ctx.NodeID, subnets.Config{}),
				prometheus.NewRegistry(),
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
					ctx.ChainID,           // Destination Chain
					requestID,             // Request ID
					tt.expectedResponseOp, // Operation
					expectedFailedMsg,     // Failure Message
					p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
				)
			}

			// Make sure we send a message to ourselves since [myNodeID]
			// is in [nodeIDs].
			router.EXPECT().HandleInternal(gomock.Any(), gomock.Any()).Do(
				func(_ context.Context, msg message.InboundMessage) {
					// Make sure we're sending ourselves the expected message.
					tt.assertMsgToMyself(require, msg)
				},
			)

			// Make sure we're making the correct outbound message.
			tt.setMsgCreatorExpect(msgCreator)

			// Make sure we're sending the message
			tt.setExternalSenderExpect(externalSender)

			tt.sendF(require, sender, nodeIDsCopy)
		})
	}
}

func TestSender_Bootstrap_Responses(t *testing.T) {
	var (
		destinationNodeID = ids.GenerateTestNodeID()
		deadline          = time.Second
		requestID         = uint32(1337)
		summaryIDs        = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
		summary           = []byte{1, 2, 3}
	)
	snowCtx := snowtest.Context(t, snowtest.PChainID)
	ctx := snowtest.ConsensusContext(snowCtx)

	type test struct {
		name                    string
		assertMsgToMyself       func(require *require.Assertions, msg message.InboundMessage)
		setMsgCreatorExpect     func(msgCreator *messagemock.OutboundMsgBuilder)
		setExternalSenderExpect func(externalSender *sendermock.ExternalSender)
		sendF                   func(require *require.Assertions, sender common.Sender, nodeID ids.NodeID)
	}

	tests := []test{
		{
			name: "StateSummaryFrontier",
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().StateSummaryFrontier(
					ctx.ChainID,
					requestID,
					summary,
				).Return(nil, nil) // Don't care about the message
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&p2ppb.StateSummaryFrontier{}, msg.Message())
				innerMsg := msg.Message().(*p2ppb.StateSummaryFrontier)
				require.Equal(ctx.ChainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(summary, innerMsg.Summary)
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						NodeIDs: set.Of(destinationNodeID),
					},
					ctx.SubnetID, // Subnet ID
					gomock.Any(),
				).Return(nil)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendStateSummaryFrontier(context.Background(), nodeID, requestID, summary)
			},
		},
		{
			name: "AcceptedStateSummary",
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().AcceptedStateSummary(
					ctx.ChainID,
					requestID,
					summaryIDs,
				).Return(nil, nil) // Don't care about the message
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&p2ppb.AcceptedStateSummary{}, msg.Message())
				innerMsg := msg.Message().(*p2ppb.AcceptedStateSummary)
				require.Equal(ctx.ChainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				for i, summaryID := range summaryIDs {
					require.Equal(summaryID[:], innerMsg.SummaryIds[i])
				}
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						NodeIDs: set.Of(destinationNodeID),
					},
					ctx.SubnetID, // Subnet ID
					gomock.Any(),
				).Return(nil)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendAcceptedStateSummary(context.Background(), nodeID, requestID, summaryIDs)
			},
		},
		{
			name: "AcceptedFrontier",
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().AcceptedFrontier(
					ctx.ChainID,
					requestID,
					summaryIDs[0],
				).Return(nil, nil) // Don't care about the message
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&p2ppb.AcceptedFrontier{}, msg.Message())
				innerMsg := msg.Message().(*p2ppb.AcceptedFrontier)
				require.Equal(ctx.ChainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				require.Equal(summaryIDs[0][:], innerMsg.ContainerId)
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						NodeIDs: set.Of(destinationNodeID),
					},
					ctx.SubnetID, // Subnet ID
					gomock.Any(),
				).Return(nil)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendAcceptedFrontier(context.Background(), nodeID, requestID, summaryIDs[0])
			},
		},
		{
			name: "Accepted",
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().Accepted(
					ctx.ChainID,
					requestID,
					summaryIDs,
				).Return(nil, nil) // Don't care about the message
			},
			assertMsgToMyself: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&p2ppb.Accepted{}, msg.Message())
				innerMsg := msg.Message().(*p2ppb.Accepted)
				require.Equal(ctx.ChainID[:], innerMsg.ChainId)
				require.Equal(requestID, innerMsg.RequestId)
				for i, summaryID := range summaryIDs {
					require.Equal(summaryID[:], innerMsg.ContainerIds[i])
				}
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						NodeIDs: set.Of(destinationNodeID),
					},
					ctx.SubnetID, // Subnet ID
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

			var (
				msgCreator     = messagemock.NewOutboundMsgBuilder(ctrl)
				externalSender = sendermock.NewExternalSender(ctrl)
				timeoutManager = timeoutmock.NewManager(ctrl)
				router         = routermock.NewRouter(ctrl)
			)

			sender, err := New(
				ctx,
				msgCreator,
				externalSender,
				router,
				timeoutManager,
				p2ppb.EngineType_ENGINE_TYPE_CHAIN,
				subnets.New(ctx.NodeID, subnets.Config{}),
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			// Set the timeout (deadline)
			timeoutManager.EXPECT().TimeoutDuration().Return(deadline).AnyTimes()

			// Case: sending to ourselves
			{
				router.EXPECT().HandleInternal(gomock.Any(), gomock.Any()).Do(
					func(_ context.Context, msg message.InboundMessage) {
						// Make sure we're sending ourselves the expected message.
						tt.assertMsgToMyself(require, msg)
					},
				)
				tt.sendF(require, sender, ctx.NodeID)
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
		destinationNodeID = ids.GenerateTestNodeID()
		deadline          = time.Second
		requestID         = uint32(1337)
		containerID       = ids.GenerateTestID()
		engineType        = p2ppb.EngineType_ENGINE_TYPE_CHAIN
	)
	snowCtx := snowtest.Context(t, snowtest.PChainID)
	ctx := snowtest.ConsensusContext(snowCtx)

	type test struct {
		name                    string
		failedMsgF              func(nodeID ids.NodeID) message.InboundMessage
		shouldFailMessageToSelf bool
		assertMsg               func(require *require.Assertions, msg message.InboundMessage)
		expectedResponseOp      message.Op
		setMsgCreatorExpect     func(msgCreator *messagemock.OutboundMsgBuilder)
		setExternalSenderExpect func(externalSender *sendermock.ExternalSender, sentTo set.Set[ids.NodeID])
		sendF                   func(require *require.Assertions, sender common.Sender, nodeID ids.NodeID)
		expectedEngineType      p2ppb.EngineType
	}

	tests := []test{
		{
			name: "GetAncestors",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetAncestorsFailed(
					nodeID,
					ctx.ChainID,
					requestID,
					engineType,
				)
			},
			shouldFailMessageToSelf: false,
			assertMsg: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&message.GetAncestorsFailed{}, msg.Message())
				innerMsg := msg.Message().(*message.GetAncestorsFailed)
				require.Equal(ctx.ChainID, innerMsg.ChainID)
				require.Equal(requestID, innerMsg.RequestID)
				require.Equal(engineType, innerMsg.EngineType)
			},
			expectedResponseOp: message.AncestorsOp,
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().GetAncestors(
					ctx.ChainID,
					requestID,
					deadline,
					containerID,
					engineType,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender, sentTo set.Set[ids.NodeID]) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						NodeIDs: set.Of(destinationNodeID),
					},
					ctx.SubnetID,
					gomock.Any(),
				).Return(sentTo)
			},
			sendF: func(_ *require.Assertions, sender common.Sender, nodeID ids.NodeID) {
				sender.SendGetAncestors(context.Background(), nodeID, requestID, containerID)
			},
			expectedEngineType: engineType,
		},
		{
			name: "Get",
			failedMsgF: func(nodeID ids.NodeID) message.InboundMessage {
				return message.InternalGetFailed(
					nodeID,
					ctx.ChainID,
					requestID,
				)
			},
			shouldFailMessageToSelf: true,
			assertMsg: func(require *require.Assertions, msg message.InboundMessage) {
				require.IsType(&message.GetFailed{}, msg.Message())
				innerMsg := msg.Message().(*message.GetFailed)
				require.Equal(ctx.ChainID, innerMsg.ChainID)
				require.Equal(requestID, innerMsg.RequestID)
			},
			expectedResponseOp: message.PutOp,
			setMsgCreatorExpect: func(msgCreator *messagemock.OutboundMsgBuilder) {
				msgCreator.EXPECT().Get(
					ctx.ChainID,
					requestID,
					deadline,
					containerID,
				).Return(nil, nil)
			},
			setExternalSenderExpect: func(externalSender *sendermock.ExternalSender, sentTo set.Set[ids.NodeID]) {
				externalSender.EXPECT().Send(
					gomock.Any(), // Outbound message
					common.SendConfig{
						NodeIDs: set.Of(destinationNodeID),
					},
					ctx.SubnetID,
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

			var (
				msgCreator     = messagemock.NewOutboundMsgBuilder(ctrl)
				externalSender = sendermock.NewExternalSender(ctrl)
				timeoutManager = timeoutmock.NewManager(ctrl)
				router         = routermock.NewRouter(ctrl)
			)

			// Instantiate new registerers to avoid duplicate metrics
			// registration
			ctx.Registerer = prometheus.NewRegistry()

			sender, err := New(
				ctx,
				msgCreator,
				externalSender,
				router,
				timeoutManager,
				engineType,
				subnets.New(ctx.NodeID, subnets.Config{}),
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			// Set the timeout (deadline)
			timeoutManager.EXPECT().TimeoutDuration().Return(deadline).AnyTimes()

			// Case: sending to myself
			{
				// Make sure we register requests with the router
				expectedFailedMsg := tt.failedMsgF(ctx.NodeID)
				router.EXPECT().RegisterRequest(
					gomock.Any(),          // Context
					ctx.NodeID,            // Node ID
					ctx.ChainID,           // Destination Chain
					requestID,             // Request ID
					tt.expectedResponseOp, // Operation
					expectedFailedMsg,     // Failure Message
					tt.expectedEngineType, // Engine Type
				)

				if tt.shouldFailMessageToSelf {
					router.EXPECT().HandleInternal(gomock.Any(), gomock.Any()).Do(
						func(_ context.Context, msg message.InboundMessage) {
							// Make sure we're sending ourselves the expected message.
							tt.assertMsg(require, msg)
						},
					)
				}

				tt.sendF(require, sender, ctx.NodeID)
			}

			// Case: Node is benched
			{
				timeoutManager.EXPECT().IsBenched(destinationNodeID, ctx.ChainID).Return(true)

				timeoutManager.EXPECT().RegisterRequestToUnreachableValidator()

				// Make sure we register requests with the router
				expectedFailedMsg := tt.failedMsgF(destinationNodeID)
				router.EXPECT().RegisterRequest(
					gomock.Any(),          // Context
					destinationNodeID,     // Node ID
					ctx.ChainID,           // Destination Chain
					requestID,             // Request ID
					tt.expectedResponseOp, // Operation
					expectedFailedMsg,     // Failure Message
					tt.expectedEngineType, // Engine Type
				)

				router.EXPECT().HandleInternal(gomock.Any(), gomock.Any()).Do(
					func(_ context.Context, msg message.InboundMessage) {
						// Make sure we're sending ourselves the expected message.
						tt.assertMsg(require, msg)
					},
				)

				tt.sendF(require, sender, destinationNodeID)
			}

			// Case: Node is not myself, not benched and send fails
			{
				timeoutManager.EXPECT().IsBenched(destinationNodeID, ctx.ChainID).Return(false)

				timeoutManager.EXPECT().RegisterRequestToUnreachableValidator()

				// Make sure we register requests with the router
				expectedFailedMsg := tt.failedMsgF(destinationNodeID)
				router.EXPECT().RegisterRequest(
					gomock.Any(),          // Context
					destinationNodeID,     // Node ID
					ctx.ChainID,           // Destination Chain
					requestID,             // Request ID
					tt.expectedResponseOp, // Operation
					expectedFailedMsg,     // Failure Message
					tt.expectedEngineType, // Engine Type
				)

				router.EXPECT().HandleInternal(gomock.Any(), gomock.Any()).Do(
					func(_ context.Context, msg message.InboundMessage) {
						// Make sure we're sending ourselves the expected message.
						tt.assertMsg(require, msg)
					},
				)

				// Make sure we're making the correct outbound message.
				tt.setMsgCreatorExpect(msgCreator)

				// Make sure we're sending the message
				tt.setExternalSenderExpect(externalSender, set.Set[ids.NodeID]{})

				tt.sendF(require, sender, destinationNodeID)
			}
		})
	}
}

func noopSubscription(ctx context.Context) (common.Message, error) {
	<-ctx.Done()
	return common.Message(0), ctx.Err()
}
