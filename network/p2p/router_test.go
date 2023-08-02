// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/mocks"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestAppRequestResponse(t *testing.T) {
	handlerID := uint64(0x0)
	request := []byte("request")
	response := []byte("response")
	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()

	tests := []struct {
		name        string
		requestFunc func(t *testing.T, router *Router, client *Client, sender *common.MockSender, handler *mocks.MockHandler, wg *sync.WaitGroup)
	}{
		{
			name: "app request",
			requestFunc: func(t *testing.T, router *Router, client *Client, sender *common.MockSender, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.EXPECT().SendAppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) {
						for range nodeIDs {
							go func() {
								require.NoError(t, router.AppRequest(ctx, nodeID, requestID, time.Time{}, request))
							}()
						}
					}).AnyTimes()
				sender.EXPECT().SendAppResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, _ ids.NodeID, requestID uint32, response []byte) {
						go func() {
							require.NoError(t, router.AppResponse(ctx, nodeID, requestID, response))
						}()
					}).AnyTimes()
				handler.EXPECT().
					AppRequest(context.Background(), nodeID, gomock.Any(), request).
					DoAndReturn(func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, error) {
						return response, nil
					})

				callback := func(actualNodeID ids.NodeID, actualResponse []byte, err error) {
					defer wg.Done()

					require.NoError(t, err)
					require.Equal(t, nodeID, actualNodeID)
					require.Equal(t, response, actualResponse)
				}

				require.NoError(t, client.AppRequestAny(context.Background(), request, callback))
			},
		},
		{
			name: "app request failed",
			requestFunc: func(t *testing.T, router *Router, client *Client, sender *common.MockSender, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.EXPECT().SendAppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) {
						for range nodeIDs {
							go func() {
								require.NoError(t, router.AppRequestFailed(ctx, nodeID, requestID))
							}()
						}
					})

				callback := func(actualNodeID ids.NodeID, actualResponse []byte, err error) {
					defer wg.Done()

					require.ErrorIs(t, err, ErrAppRequestFailed)
					require.Equal(t, nodeID, actualNodeID)
					require.Nil(t, actualResponse)
				}

				require.NoError(t, client.AppRequest(context.Background(), set.Set[ids.NodeID]{nodeID: struct{}{}}, request, callback))
			},
		},
		{
			name: "cross-chain app request",
			requestFunc: func(t *testing.T, router *Router, client *Client, sender *common.MockSender, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				chainID := ids.GenerateTestID()
				sender.EXPECT().SendCrossChainAppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, chainID ids.ID, requestID uint32, request []byte) {
						go func() {
							require.NoError(t, router.CrossChainAppRequest(ctx, chainID, requestID, time.Time{}, request))
						}()
					}).AnyTimes()
				sender.EXPECT().SendCrossChainAppResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) {
						go func() {
							require.NoError(t, router.CrossChainAppResponse(ctx, chainID, requestID, response))
						}()
					}).AnyTimes()
				handler.EXPECT().
					CrossChainAppRequest(context.Background(), chainID, gomock.Any(), request).
					DoAndReturn(func(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
						return response, nil
					})

				callback := func(actualChainID ids.ID, actualResponse []byte, err error) {
					defer wg.Done()
					require.NoError(t, err)
					require.Equal(t, chainID, actualChainID)
					require.Equal(t, response, actualResponse)
				}

				require.NoError(t, client.CrossChainAppRequest(context.Background(), chainID, request, callback))
			},
		},
		{
			name: "cross-chain app request failed",
			requestFunc: func(t *testing.T, router *Router, client *Client, sender *common.MockSender, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.EXPECT().SendCrossChainAppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, chainID ids.ID, requestID uint32, request []byte) {
						go func() {
							require.NoError(t, router.CrossChainAppRequestFailed(ctx, chainID, requestID))
						}()
					})

				callback := func(actualChainID ids.ID, actualResponse []byte, err error) {
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
			requestFunc: func(t *testing.T, router *Router, client *Client, sender *common.MockSender, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, gossip []byte) {
						go func() {
							require.NoError(t, router.AppGossip(ctx, nodeID, gossip))
						}()
					}).AnyTimes()
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
			requestFunc: func(t *testing.T, router *Router, client *Client, sender *common.MockSender, handler *mocks.MockHandler, wg *sync.WaitGroup) {
				sender.EXPECT().SendAppGossipSpecific(gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, nodeIDs set.Set[ids.NodeID], gossip []byte) {
						for n := range nodeIDs {
							nodeID := n
							go func() {
								require.NoError(t, router.AppGossip(ctx, nodeID, gossip))
							}()
						}
					}).AnyTimes()
				handler.EXPECT().
					AppGossip(context.Background(), nodeID, request).
					DoAndReturn(func(context.Context, ids.NodeID, []byte) error {
						defer wg.Done()
						return nil
					})

				require.NoError(t, client.AppGossipSpecific(context.Background(), set.Set[ids.NodeID]{nodeID: struct{}{}}, request))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			sender := common.NewMockSender(ctrl)
			handler := mocks.NewMockHandler(ctrl)
			router := NewRouter(logging.NoLog{}, sender)
			require.NoError(router.Connected(context.Background(), nodeID, nil))

			client, err := router.RegisterAppProtocol(handlerID, handler)
			require.NoError(err)

			wg := &sync.WaitGroup{}
			wg.Add(1)
			tt.requestFunc(t, router, client, sender, handler, wg)
			wg.Wait()
		})
	}
}

func TestRouterDropMessage(t *testing.T) {
	unregistered := byte(0x0)

	tests := []struct {
		name        string
		requestFunc func(router *Router) error
		err         error
	}{
		{
			name: "drop unregistered app request message",
			requestFunc: func(router *Router) error {
				return router.AppRequest(context.Background(), ids.GenerateTestNodeID(), 0, time.Time{}, []byte{unregistered})
			},
			err: nil,
		},
		{
			name: "drop empty app request message",
			requestFunc: func(router *Router) error {
				return router.AppRequest(context.Background(), ids.GenerateTestNodeID(), 0, time.Time{}, []byte{})
			},
			err: nil,
		},
		{
			name: "drop unregistered cross-chain app request message",
			requestFunc: func(router *Router) error {
				return router.CrossChainAppRequest(context.Background(), ids.GenerateTestID(), 0, time.Time{}, []byte{unregistered})
			},
			err: nil,
		},
		{
			name: "drop empty cross-chain app request message",
			requestFunc: func(router *Router) error {
				return router.CrossChainAppRequest(context.Background(), ids.GenerateTestID(), 0, time.Time{}, []byte{})
			},
			err: nil,
		},
		{
			name: "drop unregistered gossip message",
			requestFunc: func(router *Router) error {
				return router.AppGossip(context.Background(), ids.GenerateTestNodeID(), []byte{unregistered})
			},
			err: nil,
		},
		{
			name: "drop empty gossip message",
			requestFunc: func(router *Router) error {
				return router.AppGossip(context.Background(), ids.GenerateTestNodeID(), []byte{})
			},
			err: nil,
		},
		{
			name: "drop unrequested app request failed",
			requestFunc: func(router *Router) error {
				return router.AppRequestFailed(context.Background(), ids.GenerateTestNodeID(), 0)
			},
			err: ErrUnrequestedResponse,
		},
		{
			name: "drop unrequested app response",
			requestFunc: func(router *Router) error {
				return router.AppResponse(context.Background(), ids.GenerateTestNodeID(), 0, nil)
			},
			err: ErrUnrequestedResponse,
		},
		{
			name: "drop unrequested cross-chain request failed",
			requestFunc: func(router *Router) error {
				return router.CrossChainAppRequestFailed(context.Background(), ids.GenerateTestID(), 0)
			},
			err: ErrUnrequestedResponse,
		},
		{
			name: "drop unrequested cross-chain response",
			requestFunc: func(router *Router) error {
				return router.CrossChainAppResponse(context.Background(), ids.GenerateTestID(), 0, nil)
			},
			err: ErrUnrequestedResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			router := NewRouter(logging.NoLog{}, nil)

			err := tt.requestFunc(router)
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
	sender := common.NewMockSender(ctrl)
	router := NewRouter(logging.NoLog{}, sender)
	nodeID := ids.GenerateTestNodeID()

	requestSent := &sync.WaitGroup{}
	sender.EXPECT().SendAppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Do(func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) {
			for range nodeIDs {
				requestSent.Add(1)
				go func() {
					require.NoError(router.AppRequest(ctx, nodeID, requestID, time.Time{}, request))
					requestSent.Done()
				}()
			}
		}).AnyTimes()

	timeout := &sync.WaitGroup{}
	response := []byte("response")
	handler.EXPECT().AppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, request []byte) ([]byte, error) {
			timeout.Wait()
			return response, nil
		}).AnyTimes()
	sender.EXPECT().SendAppResponse(gomock.Any(), gomock.Any(), gomock.Any(), response)

	client, err := router.RegisterAppProtocol(0x1, handler)
	require.NoError(err)

	require.NoError(client.AppRequest(context.Background(), set.Set[ids.NodeID]{nodeID: struct{}{}}, []byte{}, nil))
	requestSent.Wait()

	// force the router to use the same requestID
	router.requestID = 0
	timeout.Add(1)
	err = client.AppRequest(context.Background(), set.Set[ids.NodeID]{nodeID: struct{}{}}, []byte{}, nil)
	requestSent.Wait()
	require.ErrorIs(err, ErrRequestPending)

	timeout.Done()
}

func TestRouterConnected(t *testing.T) {
	tests := []struct {
		name       string
		connect    []ids.NodeID
		disconnect []ids.NodeID
	}{
		{
			name: "empty",
		},
		{
			name: "connect and disconnect",
			connect: []ids.NodeID{
				{0x0},
			},
			disconnect: []ids.NodeID{
				{0x0},
			},
		},
		{
			name: "two nodes connect",
			connect: []ids.NodeID{
				{0x0, 0x1},
			},
		},
		{
			name: "two nodes connect, last one disconnects",
			connect: []ids.NodeID{
				{0x0, 0x1},
			},
			disconnect: []ids.NodeID{
				{0x1},
			},
		},
		{
			name: "two nodes connect, first one disconnects",
			connect: []ids.NodeID{
				{0x0, 0x1},
			},
			disconnect: []ids.NodeID{
				{0x0},
			},
		},
		{
			name: "two nodes connect and disconnect",
			connect: []ids.NodeID{
				{0x0, 0x1},
			},
			disconnect: []ids.NodeID{
				{0x0, 0x1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			router := NewRouter(logging.NoLog{}, nil)

			expected := set.Set[ids.NodeID]{}

			for _, connect := range tt.connect {
				expected.Add(connect)
				require.NoError(router.Connected(context.Background(), connect, nil))
			}

			for _, disconnect := range tt.disconnect {
				expected.Remove(disconnect)
				require.NoError(router.Disconnected(context.Background(), disconnect))
			}

			require.Len(expected, router.peers.Len())
			for _, peer := range router.peers.List() {
				require.Contains(expected, peer)
			}
		})
	}
}
