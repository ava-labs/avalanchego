// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// timePtr is a helper function to convert a time.Duration to a pointer
func timePtr(d time.Duration) *time.Duration {
	return &d
}

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
			name:            "first sample - insufficient data",
			completed:       0,
			timestamp:       now,
			expectedEta:     nil,
			expectedPercent: 0.0,
		},
		{
			// should return nil ETA and 0% complete (rate is 10 per second)
			name:            "second sample - insufficient data",
			completed:       100,
			timestamp:       now.Add(10 * time.Second),
			expectedEta:     nil,
			expectedPercent: 0.0,
		},
		{
			// should return 80s ETA and 20% complete (rate still 10 per second)
			name:            "third sample - sufficient data for ETA",
			completed:       200,
			timestamp:       now.Add(20 * time.Second),
			expectedEta:     timePtr(80 * time.Second),
			expectedPercent: 20.0,
		},
		{
			// should return 24s ETA and 60% complete (rate is 15 per second)
			name:            "fourth sample - non linear since we sped up",
			completed:       600,
			timestamp:       now.Add(40 * time.Second),
			expectedEta:     timePtr(24 * time.Second),
			expectedPercent: 60.0,
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
