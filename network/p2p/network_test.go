// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/logging"
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
	ctx := t.Context()
	wantNodeID := ids.GenerateTestNodeID()
	wantMsg := []byte("message")

	var appGossipCalled, appRequestCalled bool
	testHandler := &TestHandler{
		AppGossipF: func(_ context.Context, nodeID ids.NodeID, msg []byte) {
			appGossipCalled = true
			require.Equal(wantNodeID, nodeID)
			require.Equal(wantMsg, msg)
		},
		AppRequestF: func(_ context.Context, nodeID ids.NodeID, _ time.Time, msg []byte) ([]byte, *common.AppError) {
			appRequestCalled = true
			require.Equal(wantNodeID, nodeID)
			require.Equal(wantMsg, msg)
			return nil, nil
		},
	}

	sender := &enginetest.SenderStub{
		SentAppGossip:  make(chan []byte, 1),
		SentAppRequest: make(chan []byte, 1),
	}

	network, err := NewNetwork(
		logging.NoLog{},
		sender,
		prometheus.NewRegistry(),
		"",
	)
	require.NoError(err)
	require.NoError(network.AddHandler(1, testHandler))
	client := network.NewClient(1, PeerSampler{Peers: &Peers{}})

	require.NoError(client.AppGossip(
		ctx,
		common.SendConfig{
			Peers: 1,
		},
		wantMsg,
	))
	require.NoError(network.AppGossip(ctx, wantNodeID, <-sender.SentAppGossip))
	require.True(appGossipCalled)

	require.NoError(client.AppRequest(ctx, set.Of(ids.EmptyNodeID), wantMsg, func(context.Context, ids.NodeID, []byte, error) {}))
	require.NoError(network.AppRequest(ctx, wantNodeID, 1, time.Time{}, <-sender.SentAppRequest))
	require.True(appRequestCalled)
}

// Tests that the Client prefixes messages with the handler prefix
func TestClientPrefixesMessages(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	sender := enginetest.SenderStub{
		SentAppRequest: make(chan []byte, 1),
		SentAppGossip:  make(chan []byte, 1),
	}

	peers := &Peers{}
	network, err := NewNetwork(
		logging.NoLog{},
		sender,
		prometheus.NewRegistry(),
		"",
		peers,
	)
	require.NoError(err)
	require.NoError(network.Connected(ctx, ids.EmptyNodeID, nil))
	client := network.NewClient(handlerID, PeerSampler{Peers: peers})

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

	require.NoError(client.AppGossip(
		ctx,
		common.SendConfig{
			Peers: 1,
		},
		want,
	))
	gotAppGossip := <-sender.SentAppGossip
	require.Equal(handlerPrefix, gotAppGossip[0])
	require.Equal(want, gotAppGossip[1:])
}

// Tests that the Client callback is called on a successful response
func TestAppRequestResponse(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	sender := enginetest.SenderStub{
		SentAppRequest: make(chan []byte, 1),
	}
	network, err := NewNetwork(
		logging.NoLog{},
		sender,
		prometheus.NewRegistry(),
		"",
	)
	require.NoError(err)
	client := network.NewClient(handlerID, PeerSampler{Peers: &Peers{}})

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

// Tests that the Client does not provide a cancelled context to the AppSender.
func TestAppRequestCancelledContext(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	sentMessages := make(chan []byte, 1)
	sender := &enginetest.Sender{
		SendAppRequestF: func(ctx context.Context, _ set.Set[ids.NodeID], _ uint32, msgBytes []byte) error {
			require.NoError(ctx.Err())
			sentMessages <- msgBytes
			return nil
		},
	}
	network, err := NewNetwork(
		logging.NoLog{},
		sender,
		prometheus.NewRegistry(),
		"",
	)
	require.NoError(err)
	client := network.NewClient(handlerID, PeerSampler{Peers: &Peers{}})

	wantResponse := []byte("response")
	wantNodeID := ids.GenerateTestNodeID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotNodeID ids.NodeID, gotResponse []byte, err error) {
		require.Equal(wantNodeID, gotNodeID)
		require.NoError(err)
		require.Equal(wantResponse, gotResponse)

		close(done)
	}

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	want := []byte("request")
	require.NoError(client.AppRequest(cancelledCtx, set.Of(wantNodeID), want, callback))
	got := <-sentMessages
	require.Equal(handlerPrefix, got[0])
	require.Equal(want, got[1:])

	require.NoError(network.AppResponse(ctx, wantNodeID, 1, wantResponse))
	<-done
}

// Tests that the Client callback is given an error if the request fails
func TestAppRequestFailed(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	sender := enginetest.SenderStub{
		SentAppRequest: make(chan []byte, 1),
	}
	network, err := NewNetwork(
		logging.NoLog{},
		sender,
		prometheus.NewRegistry(),
		"",
	)
	require.NoError(err)
	client := network.NewClient(handlerID, PeerSampler{Peers: &Peers{}})

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

// Messages for unregistered handlers should be dropped gracefully
func TestAppGossipMessageForUnregisteredHandler(t *testing.T) {
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
			ctx := t.Context()
			handler := &TestHandler{
				AppGossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
			}
			network, err := NewNetwork(
				logging.NoLog{},
				nil,
				prometheus.NewRegistry(),
				"",
			)
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))
			require.NoError(network.AppGossip(ctx, ids.EmptyNodeID, tt.msg))
		})
	}
}

// An unregistered handler should gracefully drop messages by responding
// to the requester with a common.AppError
func TestAppRequestMessageForUnregisteredHandler(t *testing.T) {
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
			ctx := t.Context()
			handler := &TestHandler{
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
					require.Fail("should not be called")
					return nil, nil
				},
			}

			wantNodeID := ids.GenerateTestNodeID()
			wantRequestID := uint32(111)

			done := make(chan struct{})
			sender := &enginetest.Sender{}
			sender.SendAppErrorF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
				defer close(done)

				require.Equal(wantNodeID, nodeID)
				require.Equal(wantRequestID, requestID)
				require.Equal(ErrUnregisteredHandler.Code, errorCode)
				require.Equal(ErrUnregisteredHandler.Message, errorMessage)

				return nil
			}
			network, err := NewNetwork(
				logging.NoLog{},
				sender,
				prometheus.NewRegistry(),
				"",
			)
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))

			require.NoError(network.AppRequest(ctx, wantNodeID, wantRequestID, time.Time{}, tt.msg))
			<-done
		})
	}
}

// A handler that errors should send an AppError to the requesting peer
func TestAppError(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()
	appError := &common.AppError{
		Code:    123,
		Message: "foo",
	}
	handler := &TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			return nil, appError
		},
	}

	wantNodeID := ids.GenerateTestNodeID()
	wantRequestID := uint32(111)

	done := make(chan struct{})
	sender := &enginetest.Sender{}
	sender.SendAppErrorF = func(_ context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
		defer close(done)

		require.Equal(wantNodeID, nodeID)
		require.Equal(wantRequestID, requestID)
		require.Equal(appError.Code, errorCode)
		require.Equal(appError.Message, errorMessage)

		return nil
	}
	network, err := NewNetwork(
		logging.NoLog{},
		sender,
		prometheus.NewRegistry(),
		"",
	)
	require.NoError(err)
	require.NoError(network.AddHandler(handlerID, handler))
	msg := PrefixMessage(ProtocolPrefix(handlerID), []byte("message"))

	require.NoError(network.AppRequest(ctx, wantNodeID, wantRequestID, time.Time{}, msg))
	<-done
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
			ctx := t.Context()
			handler := &TestHandler{
				AppGossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
					require.Fail("should not be called")
					return nil, nil
				},
			}
			network, err := NewNetwork(
				logging.NoLog{},
				nil,
				prometheus.NewRegistry(),
				"",
			)
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))

			err = network.AppResponse(ctx, ids.EmptyNodeID, 0, []byte("foobar"))
			require.ErrorIs(err, ErrUnrequestedResponse)
			err = network.AppRequestFailed(ctx, ids.EmptyNodeID, 0, common.ErrTimeout)
			require.ErrorIs(err, ErrUnrequestedResponse)
		})
	}
}

// It's possible for the request id to overflow and wrap around.
// If there are still pending requests with the same request id, we should
// not attempt to issue another request until the previous one has cleared.
func TestAppRequestDuplicateRequestIDs(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	sender := &enginetest.SenderStub{
		SentAppRequest: make(chan []byte, 1),
	}

	network, err := NewNetwork(
		logging.NoLog{},
		sender,
		prometheus.NewRegistry(),
		"",
	)
	require.NoError(err)
	client := network.NewClient(0x1, PeerSampler{Peers: &Peers{}})

	noOpCallback := func(context.Context, ids.NodeID, []byte, error) {}
	// create a request that never gets a response
	network.router.requestID = 1
	require.NoError(client.AppRequest(ctx, set.Of(ids.EmptyNodeID), []byte{}, noOpCallback))
	<-sender.SentAppRequest

	// force the network to use the same requestID
	network.router.requestID = 1
	err = client.AppRequest(t.Context(), set.Of(ids.EmptyNodeID), []byte{}, noOpCallback)
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

			peers := &Peers{}
			network, err := NewNetwork(
				logging.NoLog{},
				&enginetest.SenderStub{},
				prometheus.NewRegistry(),
				"",
				peers,
			)
			require.NoError(err)

			for connected := range tt.connected {
				require.NoError(network.Connected(t.Context(), connected, nil))
			}

			for disconnected := range tt.disconnected {
				require.NoError(network.Disconnected(t.Context(), disconnected))
			}

			sampleable := set.Set[ids.NodeID]{}
			sampleable.Union(tt.connected)
			sampleable.Difference(tt.disconnected)
			require.Equal(sampleable.Len(), peers.Len())

			sampled := peers.Sample(tt.limit)
			require.Len(sampled, min(tt.limit, len(sampleable)))
			require.Subset(sampleable, sampled)
		})
	}
}

func TestAppRequestAnyWithPeerSampling(t *testing.T) {
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
			sender := &enginetest.Sender{
				SendAppRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
					sent = nodeIDs
					return nil
				},
			}

			peers := &Peers{}
			n, err := NewNetwork(
				logging.NoLog{},
				sender,
				prometheus.NewRegistry(),
				"",
				peers,
			)
			require.NoError(err)
			for _, peer := range tt.peers {
				require.NoError(n.Connected(t.Context(), peer, &version.Application{}))
			}

			client := n.NewClient(1, PeerSampler{Peers: peers})

			err = client.AppRequestAny(t.Context(), []byte("foobar"), nil)
			require.ErrorIs(err, tt.expected)
			require.Subset(tt.peers, sent.List())
		})
	}
}

func TestAppRequestAnyWithValidatorSampling(t *testing.T) {
	nodeID0 := ids.GenerateTestNodeID()
	nodeID1 := ids.GenerateTestNodeID()

	tests := []struct {
		name        string
		peers       []ids.NodeID
		validators  []ids.NodeID
		expected    []ids.NodeID
		expectedErr error
	}{
		{
			name:       "validator connected",
			peers:      []ids.NodeID{nodeID0, nodeID1},
			validators: []ids.NodeID{nodeID1},
			expected:   []ids.NodeID{nodeID1},
		},
		{
			name:        "validator disconnected",
			peers:       []ids.NodeID{nodeID0},
			validators:  []ids.NodeID{nodeID1},
			expectedErr: ErrNoPeers,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput)
			for _, nodeID := range tt.validators {
				validatorSet[nodeID] = &validators.GetValidatorOutput{
					NodeID: nodeID,
					Weight: 1,
				}
			}

			state := &validatorstest.State{
				GetCurrentHeightF: func(context.Context) (uint64, error) {
					return 0, nil
				},
				GetValidatorSetF: func(
					context.Context,
					uint64,
					ids.ID,
				) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
					return validatorSet, nil
				},
			}

			validators := NewValidators(logging.NoLog{}, ids.Empty, state, 0)

			done := make(chan struct{})
			sender := &enginetest.Sender{
				SendAppRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
					require.Subset(tt.expected, nodeIDs.List())
					close(done)
					return nil
				},
			}
			network, err := NewNetwork(
				logging.NoLog{},
				sender,
				prometheus.NewRegistry(),
				"",
				validators,
			)
			require.NoError(err)
			ctx := t.Context()
			for _, peer := range tt.peers {
				require.NoError(network.Connected(ctx, peer, nil))
			}

			client := network.NewClient(0, validators)

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

	n, err := NewNetwork(
		logging.NoLog{},
		&enginetest.Sender{},
		prometheus.NewRegistry(),
		"",
	)
	require.NoError(err)
	_ = n.NewClient(0, PeerSampler{Peers: &Peers{}})
	_ = n.NewClient(0, PeerSampler{Peers: &Peers{}})
}

func TestNetworkValidators_ConnectAndDisconnect(t *testing.T) {
	tests := []struct {
		name                     string
		maxValidatorSetStaleness time.Duration
		// These arrays must be of the same length
		validators              [][]ids.NodeID
		connectedPeers          [][]ids.NodeID
		disconnectedPeers       [][]ids.NodeID
		wantConnectedValidators [][]ids.NodeID
	}{
		{
			name: "has validators and no peers",
			validators: [][]ids.NodeID{
				{{1}, {2}, {3}},
			},
			connectedPeers: [][]ids.NodeID{
				{},
			},
			disconnectedPeers: [][]ids.NodeID{
				{},
			},
			wantConnectedValidators: [][]ids.NodeID{
				{},
			},
		},
		{
			name: "has no validators and peers",
			validators: [][]ids.NodeID{
				{},
			},
			connectedPeers: [][]ids.NodeID{
				{{1}, {2}, {3}},
			},
			disconnectedPeers: [][]ids.NodeID{
				{},
			},
			wantConnectedValidators: [][]ids.NodeID{
				{},
			},
		},
		{
			name: "has connected validator",
			validators: [][]ids.NodeID{
				{{1}, {2}, {3}},
			},
			connectedPeers: [][]ids.NodeID{
				{{1}},
			},
			disconnectedPeers: [][]ids.NodeID{
				{},
			},
			wantConnectedValidators: [][]ids.NodeID{
				{{1}},
			},
		},
		{
			name: "connected validator disconnects",
			validators: [][]ids.NodeID{
				{{1}, {2}, {3}},
			},
			connectedPeers: [][]ids.NodeID{
				{{1}},
			},
			disconnectedPeers: [][]ids.NodeID{
				{{1}},
			},
			wantConnectedValidators: [][]ids.NodeID{
				{},
			},
		},
		{
			name: "one connected validator + one connected validator disconnects + one disconnected validator",
			validators: [][]ids.NodeID{
				{{1}, {2}, {3}},
			},
			connectedPeers: [][]ids.NodeID{
				{{1}, {2}},
			},
			disconnectedPeers: [][]ids.NodeID{
				{{2}},
			},
			wantConnectedValidators: [][]ids.NodeID{
				{{1}},
			},
		},
		{
			name: "disconnected validator added",
			validators: [][]ids.NodeID{
				{{1}},
				{{1}, {2}},
			},
			connectedPeers: [][]ids.NodeID{
				{{1}},
				{},
			},
			disconnectedPeers: [][]ids.NodeID{
				{},
				{},
			},
			wantConnectedValidators: [][]ids.NodeID{
				{{1}},
				{{1}},
			},
		},
		{
			name: "connected validator added",
			validators: [][]ids.NodeID{
				{{1}},
				{{1}, {2}},
			},
			connectedPeers: [][]ids.NodeID{
				{{1}, {2}},
				{},
			},
			disconnectedPeers: [][]ids.NodeID{
				{},
				{},
			},
			wantConnectedValidators: [][]ids.NodeID{
				{{1}},
				{{1}, {2}},
			},
		},
		{
			name:                     "no refresh - connected validator not added",
			maxValidatorSetStaleness: time.Hour,
			validators: [][]ids.NodeID{
				{{1}},
				{{1}, {2}},
			},
			connectedPeers: [][]ids.NodeID{
				{{1}},
				{{2}},
			},
			disconnectedPeers: [][]ids.NodeID{
				{},
				{},
			},
			wantConnectedValidators: [][]ids.NodeID{
				{{1}},
				{{1}},
			},
		},
		{
			name:                     "no refresh - disconnected validator not removed",
			maxValidatorSetStaleness: time.Hour,
			validators: [][]ids.NodeID{
				{{1}},
				{{1}, {2}},
			},
			connectedPeers: [][]ids.NodeID{
				{{1}, {2}},
				{},
			},
			disconnectedPeers: [][]ids.NodeID{
				{},
				{{2}},
			},
			wantConnectedValidators: [][]ids.NodeID{
				{{1}},
				{{1}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			if s := set.Of(
				len(tt.validators),
				len(tt.connectedPeers),
				len(tt.disconnectedPeers),
				len(tt.wantConnectedValidators),
			); s.Len() != 1 {
				require.Fail("tests vectors must be of same length")
			}

			validatorState := &validatorstest.State{}
			validatorSet := NewValidators(
				logging.NoLog{},
				ids.GenerateTestID(),
				validatorState,
				tt.maxValidatorSetStaleness,
			)

			n, err := NewNetwork(
				logging.NoLog{},
				&enginetest.Sender{},
				prometheus.NewRegistry(),
				"",
				validatorSet,
			)
			require.NoError(err)

			for i, nodeIDs := range tt.validators {
				validatorState.GetCurrentHeightF = func(_ context.Context) (
					uint64,
					error,
				) {
					return uint64(i), nil
				}

				validatorState.GetValidatorSetF = func(
					context.Context,
					uint64,
					ids.ID,
				) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
					result := make(map[ids.NodeID]*validators.GetValidatorOutput)

					for _, nodeID := range nodeIDs {
						result[nodeID] = &validators.GetValidatorOutput{
							NodeID: nodeID,
						}
					}

					return result, nil
				}

				for _, nodeID := range tt.connectedPeers[i] {
					require.NoError(n.Connected(t.Context(), nodeID, nil))
				}

				for _, nodeID := range tt.disconnectedPeers[i] {
					require.NoError(n.Disconnected(t.Context(), nodeID))
				}

				require.Equal(
					len(tt.wantConnectedValidators[i]),
					validatorSet.Len(t.Context()),
				)

				for _, nodeID := range tt.wantConnectedValidators[i] {
					require.True(validatorSet.Has(t.Context(), nodeID))
				}

				wantDisconnectedValidators := set.Of(nodeIDs...)
				wantDisconnectedValidators.Difference(set.Of(tt.wantConnectedValidators[i]...))

				for nodeID := range wantDisconnectedValidators {
					require.False(validatorSet.Has(t.Context(), nodeID))
				}

				require.Equal(
					len(tt.wantConnectedValidators[i]),
					validatorSet.Len(t.Context()),
				)
			}
		})
	}
}

func TestPeers_Has(t *testing.T) {
	require := require.New(t)

	peers := &Peers{}
	network, err := NewNetwork(
		logging.NoLog{},
		&enginetest.Sender{},
		prometheus.NewRegistry(),
		"",
		peers,
	)
	require.NoError(err)
	require.NoError(network.Connected(t.Context(), ids.EmptyNodeID, nil))

	require.True(peers.Has(ids.EmptyNodeID))
}
