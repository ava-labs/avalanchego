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
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/x/p2p/mocks"
)

func TestAppRequestResponse(t *testing.T) {
	request := []byte("request")
	response := []byte("response")
	nodeID := ids.GenerateTestNodeID()

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
					AppRequest(context.Background(), nodeID, gomock.Any(), gomock.Any(), request).
					DoAndReturn(func(context.Context, ids.NodeID, uint32, time.Time, []byte) ([]byte, error) {
						return response, nil
					})

				callback := func(actualNodeID ids.NodeID, actualResponse []byte, err error) {
					require.NoError(t, err)
					require.Equal(t, nodeID, actualNodeID)
					require.Equal(t, response, actualResponse)
					wg.Done()
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
					CrossChainAppRequest(context.Background(), chainID, gomock.Any(), gomock.Any(), request).
					DoAndReturn(func(context.Context, ids.ID, uint32, time.Time, []byte) ([]byte, error) {
						return response, nil
					})

				callback := func(actualChainID ids.ID, actualResponse []byte, err error) {
					require.NoError(t, err)
					require.Equal(t, chainID, actualChainID)
					require.Equal(t, response, actualResponse)
					wg.Done()
				}

				require.NoError(t, client.CrossChainAppRequest(context.Background(), chainID, request, callback))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			router := NewRouter()
			sender := common.NewMockSender(ctrl)
			handler := mocks.NewMockHandler(ctrl)

			client, err := router.RegisterAppProtocol(0x1, handler, sender)
			require.NoError(err)

			wg := &sync.WaitGroup{}
			wg.Add(1)
			tt.requestFunc(t, router, client, sender, handler, wg)
			wg.Wait()
		})
	}
}

func TestAppRequestDroppedMessage(t *testing.T) {
	tests := []struct {
		name        string
		requestFunc func(router *Router) error
	}{
		{
			name: "drop unregistered app request message",
			requestFunc: func(router *Router) error {
				return router.AppRequest(context.Background(), ids.GenerateTestNodeID(), 0, time.Time{}, []byte{0x1})
			},
		},
		{
			name: "drop unregistered cross-chain app request message",
			requestFunc: func(router *Router) error {
				return router.CrossChainAppRequest(context.Background(), ids.GenerateTestID(), 0, time.Time{}, []byte{0x1})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter()

			err := tt.requestFunc(router)
			require.ErrorIs(t, err, ErrUnregisteredHandler)
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
	router := NewRouter()
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
	handler.EXPECT().AppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) ([]byte, error) {
			timeout.Wait()
			return response, nil
		}).AnyTimes()
	sender.EXPECT().SendAppResponse(gomock.Any(), gomock.Any(), gomock.Any(), response)

	client, err := router.RegisterAppProtocol(0x1, handler, sender)
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
