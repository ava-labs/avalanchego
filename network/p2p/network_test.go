// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/mocks"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

func TestAppRequestResponse(t *testing.T) {
	handlerID := uint64(0x0)
	request := []byte("request")
	response := []byte("response")
	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()

	ctxKey := new(string)
	ctxVal := new(string)
	*ctxKey = "foo"
	*ctxVal = "bar"

	tests := []struct {
		name        string
		requestFunc func(t *testing.T, network *Network, client *Client, sender *common.SenderTest, handler *mocks.MockHandler, wg *sync.WaitGroup)
	}{
		{
			name: "app request",
			requestFunc: func(t *testing.T, network *Network, client *Client, sender *common.SenderTest, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.SendAppRequestF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
					for range nodeIDs {
						go func() {
							require.NoError(t, network.AppRequest(ctx, nodeID, requestID, time.Time{}, request))
						}()
					}

					return nil
				}
				sender.SendAppResponseF = func(ctx context.Context, _ ids.NodeID, requestID uint32, response []byte) error {
					go func() {
						ctx = context.WithValue(ctx, ctxKey, ctxVal)
						require.NoError(t, network.AppResponse(ctx, nodeID, requestID, response))
					}()

					return nil
				}
				handler.EXPECT().
					AppRequest(context.Background(), nodeID, gomock.Any(), request).
					DoAndReturn(func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, error) {
						return response, nil
					})

				callback := func(ctx context.Context, actualNodeID ids.NodeID, actualResponse []byte, err error) {
					defer wg.Done()

					require.NoError(t, err)
					require.Equal(t, ctxVal, ctx.Value(ctxKey))
					require.Equal(t, nodeID, actualNodeID)
					require.Equal(t, response, actualResponse)
				}

				require.NoError(t, client.AppRequestAny(context.Background(), request, callback))
			},
		},
		{
			name: "app request failed",
			requestFunc: func(t *testing.T, network *Network, client *Client, sender *common.SenderTest, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.SendAppRequestF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
					for range nodeIDs {
						go func() {
							require.NoError(t, network.AppRequestFailed(ctx, nodeID, requestID))
						}()
					}

					return nil
				}

				callback := func(_ context.Context, actualNodeID ids.NodeID, actualResponse []byte, err error) {
					defer wg.Done()

					require.ErrorIs(t, err, ErrAppRequestFailed)
					require.Equal(t, nodeID, actualNodeID)
					require.Nil(t, actualResponse)
				}

				require.NoError(t, client.AppRequest(context.Background(), set.Of(nodeID), request, callback))
			},
		},
		{
			name: "cross-chain app request",
			requestFunc: func(t *testing.T, network *Network, client *Client, sender *common.SenderTest, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				chainID := ids.GenerateTestID()
				sender.SendCrossChainAppRequestF = func(ctx context.Context, chainID ids.ID, requestID uint32, request []byte) {
					go func() {
						require.NoError(t, network.CrossChainAppRequest(ctx, chainID, requestID, time.Time{}, request))
					}()
				}
				sender.SendCrossChainAppResponseF = func(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) {
					go func() {
						ctx = context.WithValue(ctx, ctxKey, ctxVal)
						require.NoError(t, network.CrossChainAppResponse(ctx, chainID, requestID, response))
					}()
				}
				handler.EXPECT().
					CrossChainAppRequest(context.Background(), chainID, gomock.Any(), request).
					DoAndReturn(func(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
						return response, nil
					})

				callback := func(ctx context.Context, actualChainID ids.ID, actualResponse []byte, err error) {
					defer wg.Done()
					require.NoError(t, err)
					require.Equal(t, ctxVal, ctx.Value(ctxKey))
					require.Equal(t, chainID, actualChainID)
					require.Equal(t, response, actualResponse)
				}

				require.NoError(t, client.CrossChainAppRequest(context.Background(), chainID, request, callback))
			},
		},
		{
			name: "cross-chain app request failed",
			requestFunc: func(t *testing.T, network *Network, client *Client, sender *common.SenderTest, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.SendCrossChainAppRequestF = func(ctx context.Context, chainID ids.ID, requestID uint32, request []byte) {
					go func() {
						require.NoError(t, network.CrossChainAppRequestFailed(ctx, chainID, requestID))
					}()
				}

				callback := func(_ context.Context, actualChainID ids.ID, actualResponse []byte, err error) {
					defer wg.Done()

					require.ErrorIs(t, err, ErrAppRequestFailed)
					require.Equal(t, chainID, actualChainID)
					require.Nil(t, actualResponse)
				}

				require.NoError(t, client.CrossChainAppRequest(context.Background(), chainID, request, callback))
			},
		},
		{
			name: "app gossip",
			requestFunc: func(t *testing.T, network *Network, client *Client, sender *common.SenderTest, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.SendAppGossipF = func(ctx context.Context, gossip []byte) error {
					go func() {
						require.NoError(t, network.AppGossip(ctx, nodeID, gossip))
					}()

					return nil
				}
				handler.EXPECT().
					AppGossip(context.Background(), nodeID, request).
					DoAndReturn(func(context.Context, ids.NodeID, []byte) error {
						defer wg.Done()
						return nil
					})

				require.NoError(t, client.AppGossip(context.Background(), request))
			},
		},
		{
			name: "app gossip specific",
			requestFunc: func(t *testing.T, network *Network, client *Client, sender *common.SenderTest, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.SendAppGossipSpecificF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], bytes []byte) error {
					for n := range nodeIDs {
						nodeID := n
						go func() {
							require.NoError(t, network.AppGossip(ctx, nodeID, bytes))
						}()
					}

					return nil
				}
				handler.EXPECT().
					AppGossip(context.Background(), nodeID, request).
					DoAndReturn(func(context.Context, ids.NodeID, []byte) error {
						defer wg.Done()
						return nil
					})

				require.NoError(t, client.AppGossipSpecific(context.Background(), set.Of(nodeID), request))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			sender := &common.SenderTest{}
			handler := mocks.NewMockHandler(ctrl)
			n := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
			require.NoError(n.Connected(context.Background(), nodeID, nil))
			client, err := n.NewAppProtocol(handlerID, handler)
			require.NoError(err)

			wg := &sync.WaitGroup{}
			wg.Add(1)
			tt.requestFunc(t, n, client, sender, handler, wg)
			wg.Wait()
		})
	}
}

func TestNetworkDropMessage(t *testing.T) {
	unregistered := byte(0x0)

	tests := []struct {
		name        string
		requestFunc func(network *Network) error
		err         error
	}{
		{
			name: "drop unregistered app request message",
			requestFunc: func(network *Network) error {
				return network.AppRequest(context.Background(), ids.GenerateTestNodeID(), 0, time.Time{}, []byte{unregistered})
			},
			err: nil,
		},
		{
			name: "drop empty app request message",
			requestFunc: func(network *Network) error {
				return network.AppRequest(context.Background(), ids.GenerateTestNodeID(), 0, time.Time{}, []byte{})
			},
			err: nil,
		},
		{
			name: "drop unregistered cross-chain app request message",
			requestFunc: func(network *Network) error {
				return network.CrossChainAppRequest(context.Background(), ids.GenerateTestID(), 0, time.Time{}, []byte{unregistered})
			},
			err: nil,
		},
		{
			name: "drop empty cross-chain app request message",
			requestFunc: func(network *Network) error {
				return network.CrossChainAppRequest(context.Background(), ids.GenerateTestID(), 0, time.Time{}, []byte{})
			},
			err: nil,
		},
		{
			name: "drop unregistered gossip message",
			requestFunc: func(network *Network) error {
				return network.AppGossip(context.Background(), ids.GenerateTestNodeID(), []byte{unregistered})
			},
			err: nil,
		},
		{
			name: "drop empty gossip message",
			requestFunc: func(network *Network) error {
				return network.AppGossip(context.Background(), ids.GenerateTestNodeID(), []byte{})
			},
			err: nil,
		},
		{
			name: "drop unrequested app request failed",
			requestFunc: func(network *Network) error {
				return network.AppRequestFailed(context.Background(), ids.GenerateTestNodeID(), 0)
			},
			err: ErrUnrequestedResponse,
		},
		{
			name: "drop unrequested app response",
			requestFunc: func(network *Network) error {
				return network.AppResponse(context.Background(), ids.GenerateTestNodeID(), 0, nil)
			},
			err: ErrUnrequestedResponse,
		},
		{
			name: "drop unrequested cross-chain request failed",
			requestFunc: func(network *Network) error {
				return network.CrossChainAppRequestFailed(context.Background(), ids.GenerateTestID(), 0)
			},
			err: ErrUnrequestedResponse,
		},
		{
			name: "drop unrequested cross-chain response",
			requestFunc: func(network *Network) error {
				return network.CrossChainAppResponse(context.Background(), ids.GenerateTestID(), 0, nil)
			},
			err: ErrUnrequestedResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			network := NewNetwork(logging.NoLog{}, &common.SenderTest{}, prometheus.NewRegistry(), "")

			err := tt.requestFunc(network)
			require.ErrorIs(err, tt.err)
		})
	}
}

// It's possible for the request id to overflow and wrap around.
// If there are still pending requests with the same request id, we should
// not attempt to issue another request until the previous one has cleared.
func TestAppRequestDuplicateRequestIDs(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	handler := mocks.NewMockHandler(ctrl)
	sender := &common.SenderTest{
		SendAppResponseF: func(context.Context, ids.NodeID, uint32, []byte) error {
			return nil
		},
	}
	network := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	nodeID := ids.GenerateTestNodeID()

	requestSent := &sync.WaitGroup{}
	sender.SendAppRequestF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
		for range nodeIDs {
			requestSent.Add(1)
			go func() {
				require.NoError(network.AppRequest(ctx, nodeID, requestID, time.Time{}, request))
				requestSent.Done()
			}()
		}

		return nil
	}

	timeout := &sync.WaitGroup{}
	response := []byte("response")
	handler.EXPECT().AppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, request []byte) ([]byte, error) {
			timeout.Wait()
			return response, nil
		}).AnyTimes()

	require.NoError(network.Connected(context.Background(), nodeID, nil))
	client, err := network.NewAppProtocol(0x1, handler)
	require.NoError(err)

	onResponse := func(ctx context.Context, nodeID ids.NodeID, got []byte, err error) {
		require.NoError(err)
		require.Equal(response, got)
	}

	require.NoError(client.AppRequest(context.Background(), set.Of(nodeID), []byte{}, onResponse))
	requestSent.Wait()

	// force the network to use the same requestID
	network.router.requestID = 1
	timeout.Add(1)
	err = client.AppRequest(context.Background(), set.Of(nodeID), []byte{}, nil)
	requestSent.Wait()
	require.ErrorIs(err, ErrRequestPending)

	timeout.Done()
}

// Sample should always return up to [limit] peers, and less if fewer than
// [limit] peers are available.
func TestPeersSample(t *testing.T) {
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	nodeID3 := ids.GenerateTestNodeID()

	tests := []struct {
		name         string
		connected    set.Set[ids.NodeID]
		disconnected set.Set[ids.NodeID]
		limit        int
	}{
		{
			name:  "no peers",
			limit: 1,
		},
		{
			name:      "one peer connected",
			connected: set.Of(nodeID1),
			limit:     1,
		},
		{
			name:      "multiple peers connected",
			connected: set.Of(nodeID1, nodeID2, nodeID3),
			limit:     1,
		},
		{
			name:         "peer connects and disconnects - 1",
			connected:    set.Of(nodeID1),
			disconnected: set.Of(nodeID1),
			limit:        1,
		},
		{
			name:         "peer connects and disconnects - 2",
			connected:    set.Of(nodeID1, nodeID2),
			disconnected: set.Of(nodeID2),
			limit:        1,
		},
		{
			name:         "peer connects and disconnects - 2",
			connected:    set.Of(nodeID1, nodeID2, nodeID3),
			disconnected: set.Of(nodeID1, nodeID2),
			limit:        1,
		},
		{
			name:      "less than limit peers",
			connected: set.Of(nodeID1, nodeID2, nodeID3),
			limit:     4,
		},
		{
			name:      "limit peers",
			connected: set.Of(nodeID1, nodeID2, nodeID3),
			limit:     3,
		},
		{
			name:      "more than limit peers",
			connected: set.Of(nodeID1, nodeID2, nodeID3),
			limit:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			network := NewNetwork(logging.NoLog{}, &common.SenderTest{}, prometheus.NewRegistry(), "")

			for connected := range tt.connected {
				require.NoError(network.Connected(context.Background(), connected, nil))
			}

			for disconnected := range tt.disconnected {
				require.NoError(network.Disconnected(context.Background(), disconnected))
			}

			sampleable := set.Set[ids.NodeID]{}
			sampleable.Union(tt.connected)
			sampleable.Difference(tt.disconnected)

			sampled := network.Peers.Sample(tt.limit)
			require.Len(sampled, math.Min(tt.limit, len(sampleable)))
			require.Subset(sampleable, sampled)
		})
	}
}

func TestAppRequestAnyNodeSelection(t *testing.T) {
	tests := []struct {
		name     string
		peers    []ids.NodeID
		expected error
	}{
		{
			name:     "no peers",
			expected: ErrNoPeers,
		},
		{
			name:  "has peers",
			peers: []ids.NodeID{ids.GenerateTestNodeID()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			sent := set.Set[ids.NodeID]{}
			sender := &common.SenderTest{
				SendAppRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
					for nodeID := range nodeIDs {
						sent.Add(nodeID)
					}
					return nil
				},
			}

			n := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
			for _, peer := range tt.peers {
				require.NoError(n.Connected(context.Background(), peer, &version.Application{}))
			}

			client, err := n.NewAppProtocol(1, nil)
			require.NoError(err)

			err = client.AppRequestAny(context.Background(), []byte("foobar"), nil)
			require.ErrorIs(err, tt.expected)
		})
	}
}

func TestNodeSamplerClientOption(t *testing.T) {
	nodeID0 := ids.GenerateTestNodeID()
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()

	tests := []struct {
		name        string
		peers       []ids.NodeID
		option      func(t *testing.T, n *Network) ClientOption
		expected    []ids.NodeID
		expectedErr error
	}{
		{
			name:  "default",
			peers: []ids.NodeID{nodeID0, nodeID1, nodeID2},
			option: func(_ *testing.T, n *Network) ClientOption {
				return clientOptionFunc(func(*clientOptions) {})
			},
			expected: []ids.NodeID{nodeID0, nodeID1, nodeID2},
		},
		{
			name:  "validator connected",
			peers: []ids.NodeID{nodeID0, nodeID1},
			option: func(t *testing.T, n *Network) ClientOption {
				state := &validators.TestState{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 0, nil
					},
					GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return map[ids.NodeID]*validators.GetValidatorOutput{
							nodeID1: nil,
						}, nil
					},
				}

				validators := NewValidators(n.Peers, n.log, ids.Empty, state, 0)
				return WithValidatorSampling(validators)
			},
			expected: []ids.NodeID{nodeID1},
		},
		{
			name:  "validator disconnected",
			peers: []ids.NodeID{nodeID0},
			option: func(t *testing.T, n *Network) ClientOption {
				state := &validators.TestState{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 0, nil
					},
					GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return map[ids.NodeID]*validators.GetValidatorOutput{
							nodeID1: nil,
						}, nil
					},
				}

				validators := NewValidators(n.Peers, n.log, ids.Empty, state, 0)
				return WithValidatorSampling(validators)
			},
			expectedErr: ErrNoPeers,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			done := make(chan struct{})
			sender := &common.SenderTest{
				SendAppRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
					require.Subset(tt.expected, nodeIDs.List())
					close(done)
					return nil
				},
			}
			network := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
			ctx := context.Background()
			for _, peer := range tt.peers {
				require.NoError(network.Connected(ctx, peer, nil))
			}

			client, err := network.NewAppProtocol(0x0, nil, tt.option(t, network))
			require.NoError(err)

			if err = client.AppRequestAny(ctx, []byte("request"), nil); err != nil {
				close(done)
			}

			require.ErrorIs(tt.expectedErr, err)
			<-done
		})
	}
}
