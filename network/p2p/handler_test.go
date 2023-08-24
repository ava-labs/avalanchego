// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"golang.org/x/time/rate"

	"github.com/ava-labs/avalanchego/ids"
)

func TestRateLimiterHandler_AppGossip(t *testing.T) {
	tests := []struct {
		name        string
		rateLimiter *rate.Limiter
		expected    error
	}{
		{
			name:        "rate limit exceeded",
			rateLimiter: rate.NewLimiter(rate.Every(time.Second), 0),
			expected:    ErrRateLimitExceeded,
		},
		{
			name:        "handler called",
			rateLimiter: rate.NewLimiter(rate.Every(time.Second), 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			handler := RateLimiterHandler{
				Handler:     NoOpHandler{},
				RateLimiter: tt.rateLimiter,
			}

			err := handler.AppGossip(context.Background(), ids.GenerateTestNodeID(), []byte("foobar"))
			require.ErrorIs(err, tt.expected)
		})
	}
}

func TestRateLimiterHandler_AppRequest(t *testing.T) {
	tests := []struct {
		name        string
		rateLimiter *rate.Limiter
		expected    error
	}{
		{
			name:        "rate limit exceeded",
			rateLimiter: rate.NewLimiter(rate.Every(time.Second), 0),
			expected:    ErrRateLimitExceeded,
		},
		{
			name:        "handler called",
			rateLimiter: rate.NewLimiter(rate.Every(time.Second), 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			handler := RateLimiterHandler{
				Handler:     NoOpHandler{},
				RateLimiter: tt.rateLimiter,
			}

			_, err := handler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Time{}, []byte("foobar"))
			require.ErrorIs(err, tt.expected)
		})
	}
}

func TestRateLimiterHandler_CrossChainAppRequest(t *testing.T) {
	tests := []struct {
		name        string
		rateLimiter *rate.Limiter
		expected    error
	}{
		{
			name:        "rate limit exceeded",
			rateLimiter: rate.NewLimiter(rate.Every(time.Second), 0),
			expected:    ErrRateLimitExceeded,
		},
		{
			name:        "handler called",
			rateLimiter: rate.NewLimiter(rate.Every(time.Second), 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			handler := RateLimiterHandler{
				Handler:     NoOpHandler{},
				RateLimiter: tt.rateLimiter,
			}

			_, err := handler.CrossChainAppRequest(context.Background(), ids.GenerateTestID(), time.Time{}, []byte("foobar"))
			require.ErrorIs(err, tt.expected)
		})
	}
}
