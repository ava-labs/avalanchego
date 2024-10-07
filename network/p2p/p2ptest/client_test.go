// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestNewClient_AppGossip(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	appGossipChan := make(chan struct{})
	testHandler := p2p.TestHandler{
		AppGossipF: func(context.Context, ids.NodeID, []byte) {
			close(appGossipChan)
		},
	}

	client := NewClient(t, ctx, testHandler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
	require.NoError(client.AppGossip(ctx, common.SendConfig{}, []byte("foobar")))
	<-appGossipChan
}

func TestNewClient_AppRequest(t *testing.T) {
	tests := []struct {
		name        string
		appResponse []byte
		appErr      error
		appRequestF func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error
	}{
		{
			name:        "AppRequest - response",
			appResponse: []byte("foobar"),
			appRequestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error {
				return client.AppRequest(ctx, set.Of(ids.GenerateTestNodeID()), []byte("foo"), onResponse)
			},
		},
		{
			name: "AppRequest - error",
			appErr: &common.AppError{
				Code:    123,
				Message: "foobar",
			},
			appRequestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error {
				return client.AppRequest(ctx, set.Of(ids.GenerateTestNodeID()), []byte("foo"), onResponse)
			},
		},
		{
			name:        "AppRequestAny - response",
			appResponse: []byte("foobar"),
			appRequestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error {
				return client.AppRequestAny(ctx, []byte("foo"), onResponse)
			},
		},
		{
			name: "AppRequestAny - error",
			appErr: &common.AppError{
				Code:    123,
				Message: "foobar",
			},
			appRequestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error {
				return client.AppRequestAny(ctx, []byte("foo"), onResponse)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()

			appRequestChan := make(chan struct{})
			testHandler := p2p.TestHandler{
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
					if tt.appErr != nil {
						return nil, &common.AppError{
							Code:    123,
							Message: tt.appErr.Error(),
						}
					}

					return tt.appResponse, nil
				},
			}

			client := NewClient(t, ctx, testHandler, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
			require.NoError(tt.appRequestF(
				ctx,
				client,
				func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
					require.ErrorIs(err, tt.appErr)
					require.Equal(tt.appResponse, responseBytes)
					close(appRequestChan)
				},
			))
			<-appRequestChan
		})
	}
}
