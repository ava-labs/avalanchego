// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestThrottledHandlerAppGossip(t *testing.T) {
	tests := []struct {
		name        string
		Throttler   Throttler
		expectedErr error
	}{
		{
			name:      "throttled",
			Throttler: NewSlidingWindowThrottler(time.Second, 1),
		},
		{
			name:        "throttler errors",
			Throttler:   NewSlidingWindowThrottler(time.Second, 0),
			expectedErr: ErrThrottled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			handler := ThrottledHandler{
				Handler:   NoOpHandler{},
				Throttler: tt.Throttler,
			}
			err := handler.AppGossip(context.Background(), ids.GenerateTestNodeID(), []byte("foobar"))
			require.Error(err, tt.expectedErr)
		})
	}
}

func TestThrottledHandlerAppRequest(t *testing.T) {
	tests := []struct {
		name        string
		Throttler   Throttler
		expectedErr error
	}{
		{
			name:      "throttled",
			Throttler: NewSlidingWindowThrottler(time.Second, 1),
		},
		{
			name:      "throttler errors",
			Throttler: NewSlidingWindowThrottler(time.Second, 0),
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			handler := ThrottledHandler{
				Handler:   NoOpHandler{},
				Throttler: tt.Throttler,
			}
			_, err := handler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Time{}, []byte("foobar"))
			require.Error(err, tt.expectedErr)
		})
	}
}
