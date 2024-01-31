// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

const (
	handlerID     = 123
	handlerPrefix = byte(handlerID)
)

var errFoo = &common.AppError{
	Code:    123,
	Message: "foo",
}

func TestMessageRouting(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	wantNodeID := ids.GenerateTestNodeID()
	wantChainID := ids.GenerateTestID()
	wantMsg := []byte("message")

	var appGossipCalled, appRequestCalled, crossChainAppRequestCalled bool
	testHandler := &TestHandler{
		AppGossipF: func(_ context.Context, nodeID ids.NodeID, msg []byte) {
			appGossipCalled = true
			require.Equal(wantNodeID, nodeID)
			require.Equal(wantMsg, msg)
		},
		AppRequestF: func(_ context.Context, nodeID ids.NodeID, _ time.Time, msg []byte) ([]byte, error) {
			appRequestCalled = true
			require.Equal(wantNodeID, nodeID)
			require.Equal(wantMsg, msg)
			return nil, nil
		},
		CrossChainAppRequestF: func(_ context.Context, chainID ids.ID, _ time.Time, msg []byte) ([]byte, error) {
			crossChainAppRequestCalled = true
			require.Equal(wantChainID, chainID)
			require.Equal(wantMsg, msg)
			return nil, nil
		},
	}

	sender := &common.FakeSender{
		SentAppGossip:            make(chan []byte, 1),
		SentAppRequest:           make(chan []byte, 1),
		SentCrossChainAppRequest: make(chan []byte, 1),
	}

	network, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(err)
	require.NoError(network.AddHandler(1, testHandler))
	client := network.NewClient(1)

	require.NoError(client.AppGossip(ctx, wantMsg))
	require.NoError(network.AppGossip(ctx, wantNodeID, <-sender.SentAppGossip))
	require.True(appGossipCalled)

	require.NoError(client.AppRequest(ctx, set.Of(ids.EmptyNodeID), wantMsg, func(context.Context, ids.NodeID, []byte, error) {}))
	require.NoError(network.AppRequest(ctx, wantNodeID, 1, time.Time{}, <-sender.SentAppRequest))
	require.True(appRequestCalled)

	require.NoError(client.CrossChainAppRequest(ctx, ids.Empty, wantMsg, func(context.Context, ids.ID, []byte, error) {}))
	require.NoError(network.CrossChainAppRequest(ctx, wantChainID, 1, time.Time{}, <-sender.SentCrossChainAppRequest))
	require.True(crossChainAppRequestCalled)
}

// Tests that the Client prefixes messages with the handler prefix
func TestClientPrefixesMessages(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := common.FakeSender{
		SentAppRequest:           make(chan []byte, 1),
		SentAppGossip:            make(chan []byte, 1),
		SentAppGossipSpecific:    make(chan []byte, 1),
		SentCrossChainAppRequest: make(chan []byte, 1),
	}

	network, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(err)
	require.NoError(network.Connected(ctx, ids.EmptyNodeID, nil))
	client := network.NewClient(handlerID)

	want := []byte("message")

	require.NoError(client.AppRequest(
		ctx,
		set.Of(ids.EmptyNodeID),
		want,
		func(context.Context, ids.NodeID, []byte, error) {},
	))
	gotAppRequest := <-sender.SentAppRequest
	require.Equal(handlerPrefix, gotAppRequest[0])
	require.Equal(want, gotAppRequest[1:])

	require.NoError(client.AppRequestAny(
		ctx,
		want,
		func(context.Context, ids.NodeID, []byte, error) {},
	))
	gotAppRequest = <-sender.SentAppRequest
	require.Equal(handlerPrefix, gotAppRequest[0])
	require.Equal(want, gotAppRequest[1:])

	require.NoError(client.CrossChainAppRequest(
		ctx,
		ids.Empty,
		want,
		func(context.Context, ids.ID, []byte, error) {},
	))
	gotCrossChainAppRequest := <-sender.SentCrossChainAppRequest
	require.Equal(handlerPrefix, gotCrossChainAppRequest[0])
	require.Equal(want, gotCrossChainAppRequest[1:])

	require.NoError(client.AppGossip(ctx, want))
	gotAppGossip := <-sender.SentAppGossip
	require.Equal(handlerPrefix, gotAppGossip[0])
	require.Equal(want, gotAppGossip[1:])

	require.NoError(client.AppGossipSpecific(ctx, set.Of(ids.EmptyNodeID), want))
	gotAppGossip = <-sender.SentAppGossipSpecific
	require.Equal(handlerPrefix, gotAppGossip[0])
	require.Equal(want, gotAppGossip[1:])
}

// Tests that the Client callback is called on a successful response
func TestAppRequestResponse(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := common.FakeSender{
		SentAppRequest: make(chan []byte, 1),
	}
	network, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(handlerID)

	wantResponse := []byte("response")
	wantNodeID := ids.GenerateTestNodeID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotNodeID ids.NodeID, gotResponse []byte, err error) {
		require.Equal(wantNodeID, gotNodeID)
		require.NoError(err)
		require.Equal(wantResponse, gotResponse)

		close(done)
	}

	want := []byte("request")
	require.NoError(client.AppRequest(ctx, set.Of(wantNodeID), want, callback))
	got := <-sender.SentAppRequest
	require.Equal(handlerPrefix, got[0])
	require.Equal(want, got[1:])

	require.NoError(network.AppResponse(ctx, wantNodeID, 1, wantResponse))
	<-done
}

// Tests that the Client callback is given an error if the request fails
func TestAppRequestFailed(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := common.FakeSender{
		SentAppRequest: make(chan []byte, 1),
	}
	network, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(handlerID)

	wantNodeID := ids.GenerateTestNodeID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotNodeID ids.NodeID, gotResponse []byte, err error) {
		require.Equal(wantNodeID, gotNodeID)
		require.ErrorIs(err, errFoo)
		require.Nil(gotResponse)

		close(done)
	}

	require.NoError(client.AppRequest(ctx, set.Of(wantNodeID), []byte("request"), callback))
	<-sender.SentAppRequest

	require.NoError(network.AppRequestFailed(ctx, wantNodeID, 1, errFoo))
	<-done
}

// Tests that the Client callback is called on a successful response
func TestCrossChainAppRequestResponse(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := common.FakeSender{
		SentCrossChainAppRequest: make(chan []byte, 1),
	}
	network, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(handlerID)

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

	sender := common.FakeSender{
		SentCrossChainAppRequest: make(chan []byte, 1),
	}
	network, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(handlerID)

	wantChainID := ids.GenerateTestID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotChainID ids.ID, gotResponse []byte, err error) {
		require.Equal(wantChainID, gotChainID)
		require.ErrorIs(err, errFoo)
		require.Nil(gotResponse)

		close(done)
	}

	require.NoError(client.CrossChainAppRequest(ctx, wantChainID, []byte("request"), callback))
	<-sender.SentCrossChainAppRequest

	require.NoError(network.CrossChainAppRequestFailed(ctx, wantChainID, 1, errFoo))
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
			handler := &TestHandler{
				AppGossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, error) {
					require.Fail("should not be called")
					return nil, nil
				},
				CrossChainAppRequestF: func(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
					require.Fail("should not be called")
					return nil, nil
				},
			}
			network, err := NewNetwork(logging.NoLog{}, nil, prometheus.NewRegistry(), "")
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))

			require.Nil(network.AppRequest(ctx, ids.EmptyNodeID, 0, time.Time{}, tt.msg))
			require.Nil(network.AppGossip(ctx, ids.EmptyNodeID, tt.msg))
			require.Nil(network.CrossChainAppRequest(ctx, ids.Empty, 0, time.Time{}, tt.msg))
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
			handler := &TestHandler{
				AppGossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, error) {
					require.Fail("should not be called")
					return nil, nil
				},
				CrossChainAppRequestF: func(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
					require.Fail("should not be called")
					return nil, nil
				},
			}
			network, err := NewNetwork(logging.NoLog{}, nil, prometheus.NewRegistry(), "")
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))

			err = network.AppResponse(ctx, ids.EmptyNodeID, 0, []byte("foobar"))
			require.ErrorIs(err, ErrUnrequestedResponse)
			err = network.AppRequestFailed(ctx, ids.EmptyNodeID, 0, common.ErrTimeout)
			require.ErrorIs(err, ErrUnrequestedResponse)
			err = network.CrossChainAppResponse(ctx, ids.Empty, 0, []byte("foobar"))
			require.ErrorIs(err, ErrUnrequestedResponse)
			err = network.CrossChainAppRequestFailed(ctx, ids.Empty, 0, common.ErrTimeout)

			require.ErrorIs(err, ErrUnrequestedResponse)
		})
	}
}

// It's possible for the request id to overflow and wrap around.
// If there are still pending requests with the same request id, we should
// not attempt to issue another request until the previous one has cleared.
func TestAppRequestDuplicateRequestIDs(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sender := &common.FakeSender{
		SentAppRequest: make(chan []byte, 1),
	}

	network, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(0x1)

	noOpCallback := func(context.Context, ids.NodeID, []byte, error) {}
	// create a request that never gets a response
	network.router.requestID = 1
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

			network, err := NewNetwork(logging.NoLog{}, &common.FakeSender{}, prometheus.NewRegistry(), "")
			require.NoError(err)

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
					sent = nodeIDs
					return nil
				},
			}

			n, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
			require.NoError(err)
			for _, peer := range tt.peers {
				require.NoError(n.Connected(context.Background(), peer, &version.Application{}))
			}

			client := n.NewClient(1)

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
			sender := &common.SenderTest{
				SendAppRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
					require.Subset(tt.expected, nodeIDs.List())
					close(done)
					return nil
				},
			}
			network, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
			require.NoError(err)
			ctx := context.Background()
			for _, peer := range tt.peers {
				require.NoError(network.Connected(ctx, peer, nil))
			}

			client := network.NewClient(0, tt.option(t, network))

			if err = client.AppRequestAny(ctx, []byte("request"), nil); err != nil {
				close(done)
			}

			require.ErrorIs(err, tt.expectedErr)
			<-done
		})
	}
}

// Tests that a given protocol can have more than one client
func TestMultipleClients(t *testing.T) {
	require := require.New(t)

	n, err := NewNetwork(logging.NoLog{}, &common.SenderTest{}, prometheus.NewRegistry(), "")
	require.NoError(err)
	_ = n.NewClient(0)
	_ = n.NewClient(0)
}
