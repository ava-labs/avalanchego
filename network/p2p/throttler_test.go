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

func TestThrottler(t *testing.T) {
	type call struct {
		tokens      int
		expectedErr bool
	}

	tests := []struct {
		name string

		refill rate.Limit
		burst  int

		calls []call
	}{
		{
			name:   "take less than burst tokens",
			refill: rate.Every(time.Second),
			burst:  10,
			calls: []call{
				{
					tokens: 9,
				},
			},
		},
		{
			name:   "take burst tokens",
			refill: rate.Every(time.Second),
			burst:  10,
			calls: []call{
				{
					tokens: 10,
				},
			},
		},
		{
			name:   "take more than burst tokens",
			refill: rate.Every(time.Second),
			burst:  10,
			calls: []call{
				{
					tokens:      11,
					expectedErr: true,
				},
			},
		},
		{
			name:   "take zero tokens",
			refill: rate.Every(time.Second),
			burst:  10,
			calls: []call{
				{
					tokens: 0,
				},
			},
		},
		{
			name:   "take negative tokens",
			refill: rate.Every(time.Second),
			burst:  10,
			calls: []call{
				{
					tokens: -1,
				},
			},
		},
		{
			name:   "take tokens twice",
			refill: rate.Every(time.Second),
			burst:  10,
			calls: []call{
				{
					tokens: 1,
				},
				{
					tokens: 1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			throttler := NewTokenBucketThrottler(tt.refill, tt.burst)

			nodeID := ids.GenerateTestNodeID()
			for _, call := range tt.calls {
				err := throttler.Throttle(context.Background(), nodeID, call.tokens)
				if call.expectedErr {
					require.NotNil(err)
				} else {
					require.Nil(err)
				}
			}
		})
	}
}
