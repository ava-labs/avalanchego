// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ Handler = (*TestHandler)(nil)

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
			handler := NewThrottlerHandler(
				TestHandler{
					AppGossipF: func(context.Context, ids.NodeID, []byte) {
						called = true
					},
				},
				tt.Throttler,
				logging.NoLog{},
			)

			handler.AppGossip(t.Context(), ids.GenerateTestNodeID(), []byte("foobar"))
			require.Equal(tt.expected, called)
		})
	}
}

func TestThrottlerHandlerAppRequest(t *testing.T) {
	tests := []struct {
		name        string
		Throttler   Throttler
		expectedErr *common.AppError
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

			handler := NewThrottlerHandler(
				NoOpHandler{},
				tt.Throttler,
				logging.NoLog{},
			)
			_, err := handler.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, []byte("foobar"))
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}
