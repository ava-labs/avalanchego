// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"google.golang.org/protobuf/proto"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ p2p.ValidatorSet = (*testValidatorSet)(nil)
	_ Gossiper         = (*testGossiper)(nil)
)

func TestGossiperShutdown(t *testing.T) {
	require := require.New(t)

	metrics := prometheus.NewRegistry()
	gossiper, err := NewPullGossiper[testTx](
		Config{},
		logging.NoLog{},
		nil,
		nil,
		metrics,
	)
	require.NoError(err)
	ctx, cancel := context.WithCancel(context.Background())

	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		Every(ctx, logging.NoLog{}, gossiper, time.Second)
		wg.Done()
	}()

	cancel()
	wg.Wait()
}

func TestGossiperGossip(t *testing.T) {
	tests := []struct {
		name                   string
		config                 HandlerConfig
		requester              []*testTx // what we have
		responder              []*testTx // what the peer we're requesting gossip from has
		expectedPossibleValues []*testTx // possible values we can have
		expectedLen            int
	}{
		{
			name: "no gossip - no one knows anything",
		},
		{
			name: "no gossip - requester knows more than responder",
			config: HandlerConfig{
				TargetResponseSize: 1024,
			},
			requester:              []*testTx{{id: ids.ID{0}}},
			expectedPossibleValues: []*testTx{{id: ids.ID{0}}},
			expectedLen:            1,
		},
		{
			name: "no gossip - requester knows everything responder knows",
			config: HandlerConfig{
				TargetResponseSize: 1024,
			},
			requester:              []*testTx{{id: ids.ID{0}}},
			responder:              []*testTx{{id: ids.ID{0}}},
			expectedPossibleValues: []*testTx{{id: ids.ID{0}}},
			expectedLen:            1,
		},
		{
			name: "gossip - requester knows nothing",
			config: HandlerConfig{
				TargetResponseSize: 1024,
			},
			responder:              []*testTx{{id: ids.ID{0}}},
			expectedPossibleValues: []*testTx{{id: ids.ID{0}}},
			expectedLen:            1,
		},
		{
			name: "gossip - requester knows less than responder",
			config: HandlerConfig{
				TargetResponseSize: 1024,
			},
			requester:              []*testTx{{id: ids.ID{0}}},
			responder:              []*testTx{{id: ids.ID{0}}, {id: ids.ID{1}}},
			expectedPossibleValues: []*testTx{{id: ids.ID{0}}, {id: ids.ID{1}}},
			expectedLen:            2,
		},
		{
			name: "gossip - target response size exceeded",
			config: HandlerConfig{
				TargetResponseSize: 32,
			},
			responder:              []*testTx{{id: ids.ID{0}}, {id: ids.ID{1}}, {id: ids.ID{2}}},
			expectedPossibleValues: []*testTx{{id: ids.ID{0}}, {id: ids.ID{1}}, {id: ids.ID{2}}},
			expectedLen:            2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			responseSender := &common.SenderTest{}
			responseNetwork := p2p.NewNetwork(logging.NoLog{}, responseSender, prometheus.NewRegistry(), "")
			responseBloom, err := NewBloomFilter(1000, 0.01)
			require.NoError(err)
			responseSet := &testSet{
				set:   set.Set[*testTx]{},
				bloom: responseBloom,
			}
			for _, item := range tt.responder {
				require.NoError(responseSet.Add(item))
			}

			handler, err := NewHandler[testTx, *testTx](
				logging.NoLog{},
				nil,
				responseSet,
				tt.config,
				prometheus.NewRegistry(),
			)
			require.NoError(err)
			require.NoError(responseNetwork.AddHandler(0x0, handler))

			requestSender := &common.SenderTest{
				SendAppRequestF: func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
					go func() {
						require.NoError(responseNetwork.AppRequest(ctx, ids.EmptyNodeID, requestID, time.Time{}, request))
					}()
					return nil
				},
			}

			requestNetwork := p2p.NewNetwork(logging.NoLog{}, requestSender, prometheus.NewRegistry(), "")
			require.NoError(requestNetwork.Connected(context.Background(), ids.EmptyNodeID, nil))

			gossiped := make(chan struct{})
			responseSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
				require.NoError(requestNetwork.AppResponse(ctx, nodeID, requestID, appResponseBytes))
				close(gossiped)
				return nil
			}

			bloom, err := NewBloomFilter(1000, 0.01)
			require.NoError(err)
			requestSet := &testSet{
				bloom: bloom,
			}
			for _, item := range tt.requester {
				require.NoError(requestSet.Add(item))
			}

			requestClient, err := requestNetwork.NewClient(0x0)
			require.NoError(err)

			config := Config{
				PollSize: 1,
			}
			gossiper, err := NewPullGossiper[testTx, *testTx](
				config,
				logging.NoLog{},
				requestSet,
				requestClient,
				prometheus.NewRegistry(),
			)
			require.NoError(err)
			received := set.Set[*testTx]{}
			requestSet.onAdd = func(tx *testTx) {
				received.Add(tx)
			}

			require.NoError(gossiper.Gossip(context.Background()))
			<-gossiped

			require.Len(requestSet.set, tt.expectedLen)
			require.Subset(tt.expectedPossibleValues, requestSet.set.List())

			// we should not receive anything that we already had before we
			// requested the gossip
			for _, tx := range tt.requester {
				require.NotContains(received, tx)
			}
		})
	}
}

func TestEvery(*testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	gossiper := &testGossiper{
		gossipF: func(context.Context) error {
			if calls >= 10 {
				cancel()
				return nil
			}

			calls++
			return nil
		},
	}

	go Every(ctx, logging.NoLog{}, gossiper, time.Millisecond)
	<-ctx.Done()
}

func TestValidatorGossiper(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()

	validators := testValidatorSet{
		validators: set.Of(nodeID),
	}

	calls := 0
	gossiper := ValidatorGossiper{
		Gossiper: &testGossiper{
			gossipF: func(context.Context) error {
				calls++
				return nil
			},
		},
		NodeID:     nodeID,
		Validators: validators,
	}

	// we are a validator, so we should request gossip
	require.NoError(gossiper.Gossip(context.Background()))
	require.Equal(1, calls)

	// we are not a validator, so we should not request gossip
	validators.validators = set.Set[ids.NodeID]{}
	require.NoError(gossiper.Gossip(context.Background()))
	require.Equal(2, calls)
}

func TestPushGossiper(t *testing.T) {
	tests := []struct {
		name   string
		cycles [][]*testTx
	}{
		{
			name: "single cycle",
			cycles: [][]*testTx{
				{
					&testTx{
						id: ids.ID{0},
					},
					&testTx{
						id: ids.ID{1},
					},
					&testTx{
						id: ids.ID{2},
					},
				},
			},
		},
		{
			name: "multiple cycles",
			cycles: [][]*testTx{
				{
					&testTx{
						id: ids.ID{0},
					},
				},
				{
					&testTx{
						id: ids.ID{1},
					},
					&testTx{
						id: ids.ID{2},
					},
				},
				{
					&testTx{
						id: ids.ID{3},
					},
					&testTx{
						id: ids.ID{4},
					},
					&testTx{
						id: ids.ID{5},
					},
				},
				{
					&testTx{
						id: ids.ID{6},
					},
					&testTx{
						id: ids.ID{7},
					},
					&testTx{
						id: ids.ID{8},
					},
					&testTx{
						id: ids.ID{9},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()

			sent := make(chan []byte, 1)
			gossiper := NewPushGossiper[*testTx](nil)
			gossiper.sender = &fakeGossipClient{
				sent: sent,
			}

			for _, gossipables := range tt.cycles {
				gossiper.Add(gossipables...)
				require.NoError(gossiper.Gossip(ctx))

				want := &sdk.PushGossip{
					Gossip: make([][]byte, 0, len(tt.cycles)),
				}

				for _, gossipable := range gossipables {
					bytes, err := gossipable.Marshal()
					require.NoError(err)

					want.Gossip = append(want.Gossip, bytes)
				}

				got := &sdk.PushGossip{}
				sentMsg := <-sent
				// remove the handler prefix
				require.NoError(proto.Unmarshal(sentMsg[1:], got))
				require.Equal(want.Gossip, got.Gossip)
			}
		})
	}
}

func TestSubscribe(t *testing.T) {
	require := require.New(t)

	sent := make(chan []byte)
	gossiper := NewPushGossiper[*testTx](nil)
	gossiper.sender = &fakeGossipClient{
		sent: sent,
	}

	ctx := context.Background()
	toGossip := make(chan *testTx)

	go Subscribe(ctx, logging.NoLog{}, gossiper, toGossip)

	tx := &testTx{id: ids.ID{1}}
	toGossip <- tx

	txBytes, err := tx.Marshal()
	require.NoError(err)
	want := [][]byte{txBytes}

	gotMsg := &sdk.PushGossip{}
	// remove the handler prefix
	sentMsg := <-sent
	require.NoError(proto.Unmarshal(sentMsg[1:], gotMsg))

	require.Equal(want, gotMsg.Gossip)
}

func TestSubscribeCloseChannel(*testing.T) {
	gossiper := &PushGossiper[*testTx]{}

	wg := &sync.WaitGroup{}
	ctx := context.Background()
	toGossip := make(chan *testTx)

	wg.Add(1)
	go func() {
		Subscribe(ctx, logging.NoLog{}, gossiper, toGossip)
		wg.Done()
	}()

	close(toGossip)
	wg.Wait()
}

func TestSubscribeCancelContext(*testing.T) {
	gossiper := &PushGossiper[*testTx]{}

	wg := &sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())
	toGossip := make(chan *testTx)

	wg.Add(1)
	go func() {
		Subscribe(ctx, logging.NoLog{}, gossiper, toGossip)
		wg.Done()
	}()

	cancel()
	wg.Wait()
}

func TestPushGossipE2E(t *testing.T) {
	require := require.New(t)

	// tx known by both the sender and the receiver which should not be
	// forwarded
	knownTx := &testTx{id: ids.GenerateTestID()}

	log := logging.NoLog{}
	bloom, err := NewBloomFilter(100, 0.01)
	require.NoError(err)
	set := &testSet{
		bloom: bloom,
	}
	require.NoError(set.Add(knownTx))

	metrics := prometheus.NewRegistry()
	handler, err := NewHandler[testTx, *testTx](
		log,
		nil,
		set,
		HandlerConfig{},
		metrics,
	)
	require.NoError(err)

	handlerID := uint64(123)
	forwarded := make(chan []byte, 1)
	handler.gossipSender = &fakeGossipClient{
		handlerID: handlerID,
		sent:      forwarded,
	}

	network := p2p.NewNetwork(log, nil, metrics, "")
	_, err = network.NewAppProtocol(handlerID, handler)
	require.NoError(err)

	sendGossiper := NewPushGossiper[*testTx](nil)
	sent := make(chan []byte, 1)
	sendGossiper.sender = &fakeGossipClient{
		handlerID: handlerID,
		sent:      sent,
	}

	want := []*testTx{
		{id: ids.GenerateTestID()},
		{id: ids.GenerateTestID()},
		{id: ids.GenerateTestID()},
	}

	// gossip both the new tx and the one the peer already knows about
	gossiped := append(want, knownTx)
	sendGossiper.Add(gossiped...)
	got := make([]*testTx, 0, len(want))
	set.onAdd = func(tx *testTx) {
		got = append(got, tx)
	}

	ctx := context.Background()
	require.NoError(sendGossiper.Gossip(ctx))

	// make sure that we only add new txs someone gossips to us
	require.NoError(network.AppGossip(ctx, ids.EmptyNodeID, <-sent))
	require.Equal(want, got)

	// make sure that we only forward txs we have not already seen before
	forwardedBytes := <-forwarded
	forwardedMsg := &sdk.PushGossip{}
	require.NoError(proto.Unmarshal(forwardedBytes[1:], forwardedMsg))
	require.Len(forwardedMsg.Gossip, len(want))

	gotForwarded := make([]*testTx, 0, len(got))
	for _, bytes := range forwardedMsg.Gossip {
		tx := &testTx{}
		require.NoError(tx.Unmarshal(bytes))
		gotForwarded = append(gotForwarded, tx)
	}

	require.Equal(want, gotForwarded)
}

type testGossiper struct {
	gossipF func(ctx context.Context) error
}

func (t *testGossiper) Gossip(ctx context.Context) error {
	return t.gossipF(ctx)
}

type testValidatorSet struct {
	validators set.Set[ids.NodeID]
}

func (t testValidatorSet) Has(_ context.Context, nodeID ids.NodeID) bool {
	return t.validators.Contains(nodeID)
}
