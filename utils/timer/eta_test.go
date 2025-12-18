// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestEtaTracker(t *testing.T) {
	tracker := NewEtaTracker(3, 1.0)
	now := time.Now()
	target := uint64(1000)

	tests := []struct {
		name            string
		completed       uint64
		timestamp       time.Time
		expectedEta     *time.Duration
		expectedPercent float64
	}{
		{
			// should return nil ETA and 0% complete
			name:            "sample 1: insufficient data",
			completed:       0,
			timestamp:       now,
			expectedEta:     nil,
			expectedPercent: 0.0,
		},
		{
			// should return nil ETA and 0% complete (rate is 10 per second)
			name:            "sample 2: insufficient data",
			completed:       100,
			timestamp:       now.Add(10 * time.Second),
			expectedEta:     nil,
			expectedPercent: 0.0,
		},
		{
			// should return 80s ETA and 20% complete (rate still 10 per second)
			name:            "sample 3: sufficient data for ETA",
			completed:       200,
			timestamp:       now.Add(20 * time.Second),
			expectedEta:     utils.PointerTo(80 * time.Second),
			expectedPercent: 20.0,
		},
		{
			// should return 24s ETA and 60% complete (rate is 15 per second)
			name:            "sample 4: non linear since we sped up",
			completed:       600,
			timestamp:       now.Add(40 * time.Second),
			expectedEta:     utils.PointerTo(24 * time.Second),
			expectedPercent: 60.0,
		},
		{
			// should return 0s ETA and 100% complete since we're at the end
			name:            "sample 5: at the end",
			completed:       1000,
			timestamp:       now.Add(1 * time.Second),
			expectedEta:     utils.PointerTo[time.Duration](0),
			expectedPercent: 100.0,
		},
		{
			// should return 0s ETA and 100% complete
			name:            "sample 6: past the end",
			completed:       2000,
			timestamp:       now.Add(1 * time.Second),
			expectedEta:     utils.PointerTo[time.Duration](0),
			expectedPercent: 100.0,
		},
		{
			name:            "bogus sample: time warp",
			completed:       100,
			timestamp:       now,
			expectedEta:     nil,
			expectedPercent: 0.0,
		},
		{
			name:            "bogus sample: no progress",
			completed:       100,
			timestamp:       now,
			expectedEta:     nil,
			expectedPercent: 0.0,
		},
	}

	for _, tt := range tests {
		eta, percentComplete := tracker.AddSample(tt.completed, target, tt.timestamp)
		require.Equal(t, tt.expectedEta == nil, eta == nil, tt.name)
		if eta != nil {
			require.InDelta(t, float64(*tt.expectedEta), float64(*eta), 0.0001, tt.name)
		}
		require.InDelta(t, tt.expectedPercent, percentComplete, 0.01, tt.name)
	}
}
