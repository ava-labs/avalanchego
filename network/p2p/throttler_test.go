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
					time:     currentWindowStartTime.Add(period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(2 * period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(2 * period).Add(time.Second),
					expected: true,
				},
			},
		},
		{
			name:  "throttled over multiple evaluation periods",
			limit: 5,
			calls: []call{
				{
					time:     currentWindowStartTime.Add(30 * time.Second),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(period).Add(1 * time.Second),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(period).Add(2 * time.Second),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(period).Add(3 * time.Second),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(period).Add(4 * time.Second),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(period).Add(30 * time.Second),
					expected: true,
				},
				{
					time: currentWindowStartTime.Add(period).Add(30 * time.Second),
				},
				{
					time:     currentWindowStartTime.Add(5 * period),
					expected: true,
				},
			},
		},
		{
			name:  "not throttled over multiple evaluation periods",
			limit: 2,
			calls: []call{
				{
					time:     currentWindowStartTime,
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(2 * period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(3 * period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(4 * period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(5 * period),
					expected: true,
				},
			},
		},
		{
			name:  "sparse hits",
			limit: 2,
			calls: []call{
				{
					time:     currentWindowStartTime,
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(2 * period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(5 * period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(6 * period),
					expected: true,
				},
				{
					time:     currentWindowStartTime.Add(7 * period),
					expected: true,
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
