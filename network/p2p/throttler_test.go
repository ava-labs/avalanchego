// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSlidingWindowThrottlerHandle(t *testing.T) {
	period := time.Minute
	previousWindowStartTime := time.Time{}
	currentWindowStartTime := previousWindowStartTime.Add(period)

	nodeID := ids.GenerateTestNodeID()

	type call struct {
		time     time.Time
		expected bool
	}

	tests := []struct {
		name  string
		limit int
		calls []call
	}{
		{
			name:  "throttled in current window",
			limit: 1,
			calls: []call{
				{
					time:     currentWindowStartTime,
					expected: true,
				},
				{
					time: currentWindowStartTime,
				},
			},
		},
		{
			name:  "throttled from previous window",
			limit: 2,
			calls: []call{
				{
					time:     currentWindowStartTime,
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(period).Add(time.Second),
					expected: true,
				},
				{
					time: currentWindowStartTime.Add(period).Add(time.Second),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			throttler := NewSlidingWindowThrottler(period, tt.limit)
			throttler.previous.start = previousWindowStartTime
			throttler.current.start = currentWindowStartTime

			for _, call := range tt.calls {
				throttler.clock.Set(call.time)
				require.Equal(call.expected, throttler.Handle(nodeID))
			}
		})
	}
}
