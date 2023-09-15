// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
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
			ctrl := gomock.NewController(t)

			responseSender := common.NewMockSender(ctrl)
			responseRouter := p2p.NewRouter(logging.NoLog{}, responseSender, prometheus.NewRegistry(), "")
			responseBloom, err := NewBloomFilter(1000, 0.01)
			require.NoError(err)
			responseSet := testSet{
				set:   set.Set[*testTx]{},
				bloom: responseBloom,
			}
			for _, item := range tt.responder {
				require.NoError(responseSet.Add(item))
			}
			peers := &p2p.Peers{}
			require.NoError(peers.Connected(context.Background(), ids.EmptyNodeID, nil))

			handler, err := NewHandler[*testTx](responseSet, tt.config, prometheus.NewRegistry())
			require.NoError(err)
			_, err = responseRouter.RegisterAppProtocol(0x0, handler, peers)
			require.NoError(err)

			requestSender := common.NewMockSender(ctrl)
			requestRouter := p2p.NewRouter(logging.NoLog{}, requestSender, prometheus.NewRegistry(), "")

			gossiped := make(chan struct{})
			requestSender.EXPECT().SendAppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) {
					go func() {
						require.NoError(responseRouter.AppRequest(ctx, ids.EmptyNodeID, requestID, time.Time{}, request))
					}()
				}).AnyTimes()

			responseSender.EXPECT().
				SendAppResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) {
					require.NoError(requestRouter.AppResponse(ctx, nodeID, requestID, appResponseBytes))
					close(gossiped)
				}).AnyTimes()

			bloom, err := NewBloomFilter(1000, 0.01)
			require.NoError(err)
			requestSet := testSet{
				set:   set.Set[*testTx]{},
				bloom: bloom,
			}
			for _, item := range tt.requester {
				require.NoError(requestSet.Add(item))
			}

			requestClient, err := requestRouter.RegisterAppProtocol(0x0, nil, peers)
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
