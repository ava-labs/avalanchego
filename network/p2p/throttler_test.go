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
	tests := []struct {
		name string

		refill rate.Limit
		burst  int

		tokens      int
		expectedErr bool
	}{
		{
			name:   "take less than burst tokens",
			refill: rate.Every(time.Second),
			burst:  10,
			tokens: 9,
		},
		{
			name:   "take burst tokens",
			refill: rate.Every(time.Second),
			burst:  10,
			tokens: 10,
		},
		{
			name:        "take more than burst tokens",
			refill:      rate.Every(time.Second),
			burst:       10,
			tokens:      11,
			expectedErr: true,
		},
		{
			name:   "take zero tokens",
			refill: rate.Every(time.Second),
			burst:  10,
			tokens: 0,
		},
		{
			name:   "take negative tokens",
			refill: rate.Every(time.Second),
			burst:  10,
			tokens: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			throttler := NewTokenBucketThrottler(tt.refill, tt.burst)

			nodeID := ids.GenerateTestNodeID()
			err := throttler.Throttle(context.Background(), nodeID, tt.tokens)
			if tt.expectedErr {
				require.NotNil(err)
			} else {
				require.Nil(err)
			}
		})
	}
}
