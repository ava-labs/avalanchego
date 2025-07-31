// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
		time      time.Time
		throttled bool
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
					time: currentWindowStartTime,
				},
				{
					time:      currentWindowStartTime,
					throttled: true,
				},
			},
		},
		{
			name:  "throttled from previous window",
			limit: 1,
			calls: []call{
				{
					time: previousWindowStartTime,
				},
				{
					time:      currentWindowStartTime,
					throttled: true,
				},
			},
		},
		{
			name:  "throttled over multiple evaluation periods",
			limit: 5,
			calls: []call{
				{
					time: currentWindowStartTime.Add(30 * time.Second),
				},
				{
					time: currentWindowStartTime.Add(period).Add(1 * time.Second),
				},
				{
					time: currentWindowStartTime.Add(period).Add(2 * time.Second),
				},
				{
					time: currentWindowStartTime.Add(period).Add(3 * time.Second),
				},
				{
					time: currentWindowStartTime.Add(period).Add(4 * time.Second),
				},
				{
					time: currentWindowStartTime.Add(period).Add(30 * time.Second),
				},
				{
					time:      currentWindowStartTime.Add(period).Add(30 * time.Second),
					throttled: true,
				},
				{
					time: currentWindowStartTime.Add(5 * period),
				},
			},
		},
		{
			name:  "one hit per period",
			limit: 2,
			calls: []call{
				{
					time: currentWindowStartTime,
				},
				{
					time: currentWindowStartTime.Add(period).Add(time.Second),
				},
				{
					time: currentWindowStartTime.Add(2 * period).Add(time.Second),
				},
				{
					time: currentWindowStartTime.Add(3 * period).Add(time.Second),
				},
				{
					time: currentWindowStartTime.Add(4 * period).Add(time.Second),
				},
			},
		},
		{
			// if too much time passes by, a current window might not be a
			// valid previous window.
			name:  "current window needs to be reset",
			limit: 1,
			calls: []call{
				{
					time: currentWindowStartTime,
				},
				{
					time: currentWindowStartTime.Add(10 * period),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			throttler := NewSlidingWindowThrottler(period, tt.limit)
			throttler.windows[throttler.current].start = currentWindowStartTime
			throttler.windows[1-throttler.current].start = previousWindowStartTime

			for _, call := range tt.calls {
				throttler.clock.Set(call.time)
				require.Equal(call.throttled, !throttler.Handle(nodeID))
			}
		})
	}
}
