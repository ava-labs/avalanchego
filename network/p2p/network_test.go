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
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
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

// Tests that the Client does not provide a cancelled context to the AppSender.
func TestAppRequestCancelledContext(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentMessages := make(chan []byte, 1)
	sender := &common.SenderTest{
		SendAppRequestF: func(ctx context.Context, _ set.Set[ids.NodeID], _ uint32, msgBytes []byte) error {
			require.NoError(ctx.Err())
			sentMessages <- msgBytes
			return nil
		},
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

// Tests that the Client does not provide a cancelled context to the AppSender.
func TestCrossChainAppRequestCancelledContext(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentMessages := make(chan []byte, 1)
	sender := &common.SenderTest{
		SendCrossChainAppRequestF: func(ctx context.Context, _ ids.ID, _ uint32, msgBytes []byte) {
			require.NoError(ctx.Err())
			sentMessages <- msgBytes
		},
	}
	network, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(handlerID)

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	wantChainID := ids.GenerateTestID()
	wantResponse := []byte("response")
	done := make(chan struct{})

	callback := func(_ context.Context, gotChainID ids.ID, gotResponse []byte, err error) {
		require.Equal(wantChainID, gotChainID)
		require.NoError(err)
		require.Equal(wantResponse, gotResponse)

		close(done)
	}

	require.NoError(client.CrossChainAppRequest(cancelledCtx, wantChainID, []byte("request"), callback))
	<-sentMessages

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

			require.NoError(network.AppRequest(ctx, ids.EmptyNodeID, 0, time.Time{}, tt.msg))
			require.NoError(network.AppGossip(ctx, ids.EmptyNodeID, tt.msg))
			require.NoError(network.CrossChainAppRequest(ctx, ids.Empty, 0, time.Time{}, tt.msg))
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

// Tests that a given protocol can have more than one client
func TestMultipleClients(t *testing.T) {
	require := require.New(t)

	n, err := NewNetwork(logging.NoLog{}, &common.SenderTest{}, prometheus.NewRegistry(), "")
	require.NoError(err)
	_ = n.NewClient(0)
	_ = n.NewClient(0)
}
