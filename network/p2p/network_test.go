// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

const handlerID = 1337

// Tests that the Client callback is called on a successful response
func TestAppRequestResponse(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := FakeSender{
		SentAppRequest: make(chan []byte, 1),
	}
	network := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")

	client, err := network.NewAppProtocol(handlerID, &NoOpHandler{})
	require.NoError(err)

	wantResponse := []byte("response")
	wantNodeID := ids.GenerateTestNodeID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotNodeID ids.NodeID, gotResponse []byte, err error) {
		require.Equal(wantNodeID, gotNodeID)
		require.NoError(err)
		require.Equal(wantResponse, gotResponse)

		close(done)
	}

	require.NoError(client.AppRequest(ctx, set.Of(wantNodeID), []byte("request"), callback))
	<-sender.SentAppRequest

	require.NoError(network.AppResponse(ctx, wantNodeID, 1, wantResponse))
	<-done
}

// Tests that the Client callback is given an error if the request fails
func TestAppRequestFailed(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := FakeSender{
		SentAppRequest: make(chan []byte, 1),
	}
	network := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")

	client, err := network.NewAppProtocol(handlerID, &NoOpHandler{})
	require.NoError(err)

	wantNodeID := ids.GenerateTestNodeID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotNodeID ids.NodeID, gotResponse []byte, err error) {
		require.Equal(wantNodeID, gotNodeID)
		require.ErrorIs(err, ErrAppRequestFailed)
		require.Nil(gotResponse)

		close(done)
	}

	require.NoError(client.AppRequest(ctx, set.Of(wantNodeID), []byte("request"), callback))
	<-sender.SentAppRequest

	require.NoError(network.AppRequestFailed(ctx, wantNodeID, 1))
	<-done
}

// Tests that the Client callback is called on a successful response
func TestCrossChainAppRequestResponse(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := FakeSender{
		SentCrossChainAppRequest: make(chan []byte, 1),
	}
	network := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")

	client, err := network.NewAppProtocol(handlerID, &NoOpHandler{})
	require.NoError(err)

	wantChainID := ids.GenerateTestID()
	wantResponse := []byte("response")
	done := make(chan struct{})

	callback := func(_ context.Context, gotChainID ids.ID, gotResponse []byte, err error) {
		require.Equal(wantChainID, gotChainID)
		require.NoError(err)
		require.Equal(wantResponse, gotResponse)

		close(done)
	}

	require.NoError(client.CrossChainAppRequest(ctx, wantChainID, []byte("request"), callback))
	<-sender.SentCrossChainAppRequest

	require.NoError(network.CrossChainAppResponse(ctx, wantChainID, 1, wantResponse))
	<-done
}

// Tests that the Client callback is given an error if the request fails
func TestCrossChainAppRequestFailed(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := FakeSender{
		SentCrossChainAppRequest: make(chan []byte, 1),
	}
	network := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")

	client, err := network.NewAppProtocol(handlerID, &NoOpHandler{})
	require.NoError(err)

	wantChainID := ids.GenerateTestID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotChainID ids.ID, gotResponse []byte, err error) {
		require.Equal(wantChainID, gotChainID)
		require.ErrorIs(err, ErrAppRequestFailed)
		require.Nil(gotResponse)

		close(done)
	}

	require.NoError(client.CrossChainAppRequest(ctx, wantChainID, []byte("request"), callback))
	<-sender.SentCrossChainAppRequest

	require.NoError(network.CrossChainAppRequestFailed(ctx, wantChainID, 1))
	<-done
}

// Messages for unregistered handlers should be dropped gracefully
func TestMessageForUnregisteredHandler(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "nil",
			msg:  nil,
		},
		{
			name: "empty",
			msg:  []byte{},
		},
		{
			name: "non-empty",
			msg:  []byte("foobar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()
			handler := &testHandler{
				appGossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
				appRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, error) {
					require.Fail("should not be called")
					return nil, nil
				},
				crossChainAppRequestF: func(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
					require.Fail("should not be called")
					return nil, nil
				},
			}
			network := NewNetwork(logging.NoLog{}, nil, prometheus.NewRegistry(), "")
			_, err := network.NewAppProtocol(handlerID, handler)
			require.NoError(err)

			require.Nil(network.AppRequest(ctx, ids.EmptyNodeID, 0, time.Time{}, []byte("foobar")))
			require.Nil(network.AppGossip(ctx, ids.EmptyNodeID, []byte("foobar")))
			require.Nil(network.CrossChainAppRequest(ctx, ids.Empty, 0, time.Time{}, []byte("foobar")))
		})
	}
}

// A response or timeout for a request we never made should return an error
func TestResponseForUnrequestedRequest(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "nil",
			msg:  nil,
		},
		{
			name: "empty",
			msg:  []byte{},
		},
		{
			name: "non-empty",
			msg:  []byte("foobar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()
			handler := &testHandler{
				appGossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
				appRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, error) {
					require.Fail("should not be called")
					return nil, nil
				},
				crossChainAppRequestF: func(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
					require.Fail("should not be called")
					return nil, nil
				},
			}
			network := NewNetwork(logging.NoLog{}, nil, prometheus.NewRegistry(), "")
			_, err := network.NewAppProtocol(handlerID, handler)
			require.NoError(err)

			require.ErrorIs(ErrUnrequestedResponse, network.AppResponse(ctx, ids.EmptyNodeID, 0, []byte("foobar")))
			require.ErrorIs(ErrUnrequestedResponse, network.AppRequestFailed(ctx, ids.EmptyNodeID, 0))

			require.ErrorIs(ErrUnrequestedResponse, network.CrossChainAppResponse(ctx, ids.Empty, 0, []byte("foobar")))
			require.ErrorIs(ErrUnrequestedResponse, network.CrossChainAppRequestFailed(ctx, ids.Empty, 0))
		})
	}
}

// It's possible for the request id to overflow and wrap around.
// If there are still pending requests with the same request id, we should
// not attempt to issue another request until the previous one has cleared.
func TestAppRequestDuplicateRequestIDs(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := &FakeSender{
		SentAppRequest: make(chan []byte, 1),
	}

	network := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	client, err := network.NewAppProtocol(0x1, &NoOpHandler{})
	require.NoError(err)

	noOpCallback := func(context.Context, ids.NodeID, []byte, error) {}
	// create a request that never gets a response
	require.NoError(client.AppRequest(ctx, set.Of(ids.EmptyNodeID), []byte{}, noOpCallback))
	<-sender.SentAppRequest

	// force the network to use the same requestID
	network.router.requestID = 1
	err = client.AppRequest(context.Background(), set.Of(ids.EmptyNodeID), []byte{}, noOpCallback)
	require.ErrorIs(err, ErrRequestPending)
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

			network := NewNetwork(logging.NoLog{}, &FakeSender{}, prometheus.NewRegistry(), "")

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
			sender := &MockSender{
				SendAppRequestF: func(_ context.Context, nodeID ids.NodeID, _ uint32, _ []byte) error {
					sent.Add(nodeID)
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
			require.Subset(tt.peers, sent.List())
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
			sender := &MockSender{
				SendAppRequestF: func(_ context.Context, nodeID ids.NodeID, _ uint32, _ []byte) error {
					require.Subset(tt.expected, []ids.NodeID{nodeID})
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
