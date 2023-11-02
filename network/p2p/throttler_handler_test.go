// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ Handler = (*testHandler)(nil)

func TestThrottlerHandlerAppGossip(t *testing.T) {
	tests := []struct {
		name      string
		Throttler Throttler
		expected  bool
	}{
		{
			name:      "not throttled",
			Throttler: NewSlidingWindowThrottler(time.Second, 1),
			expected:  true,
		},
		{
			name:      "throttled",
			Throttler: NewSlidingWindowThrottler(time.Second, 0),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			called := false
			handler := ThrottlerHandler{
				Handler: testHandler{
					appGossipF: func(context.Context, ids.NodeID, []byte) {
						called = true
					},
				},
				Throttler: tt.Throttler,
				Log:       logging.NoLog{},
			}

			handler.AppGossip(context.Background(), ids.GenerateTestNodeID(), []byte("foobar"))
			require.Equal(tt.expected, called)
		})
	}
}

func TestThrottlerHandlerAppRequest(t *testing.T) {
	tests := []struct {
		name        string
		Throttler   Throttler
		expectedErr error
	}{
		{
			name:      "not throttled",
			Throttler: NewSlidingWindowThrottler(time.Second, 1),
		},
		{
			name:        "throttled",
			Throttler:   NewSlidingWindowThrottler(time.Second, 0),
			expectedErr: ErrThrottled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			handler := ThrottlerHandler{
				Handler:   NoOpHandler{},
				Throttler: tt.Throttler,
				Log:       logging.NoLog{},
			}
			_, err := handler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Time{}, []byte("foobar"))
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

type testHandler struct {
	appGossipF            func(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte)
	appRequestF           func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error)
	crossChainAppRequestF func(ctx context.Context, chainID ids.ID, deadline time.Time, requestBytes []byte) ([]byte, error)
}

func (t testHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if t.appGossipF == nil {
		return
	}

	t.appGossipF(ctx, nodeID, gossipBytes)
}

func (t testHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	if t.appRequestF == nil {
		return nil, nil
	}

	return t.appRequestF(ctx, nodeID, deadline, requestBytes)
}

func (t testHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	if t.crossChainAppRequestF == nil {
		return nil, nil
	}

	return t.crossChainAppRequestF(ctx, chainID, deadline, requestBytes)
}
