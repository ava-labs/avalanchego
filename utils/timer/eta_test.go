// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEtaTracker(t *testing.T) {
	tracker := NewEtaTracker(3, 1.0)
	now := time.Now()
	target := uint64(1000)

	thirdSampleEta := time.Duration(80 * time.Second)
	fourthSampleEta := time.Duration(24 * time.Second)

	tests := []struct {
		name            string
		completed       uint64
		timestamp       time.Time
		expectedEta     *time.Duration
		expectedPercent float64
		description     string
	}{
		{
			name:            "first sample - insufficient data",
			completed:       0,
			timestamp:       now,
			expectedEta:     nil,
			expectedPercent: 0.0,
			description:     "should return nil ETA and 0% complete",
		},
		{
			name:            "second sample - insufficient data",
			completed:       100,
			timestamp:       now.Add(10 * time.Second),
			expectedEta:     nil,
			expectedPercent: 0.0,
			description:     "should return nil ETA and 0% complete (rate is 10 per second)",
		},
		{
			name:            "third sample - sufficient data for ETA",
			completed:       200,
			timestamp:       now.Add(20 * time.Second),
			expectedEta:     &thirdSampleEta,
			expectedPercent: 20.0,
			description:     "should return 80s ETA and 20% complete (rate still 10 per second)",
		},
		{
			name:            "fourth sample - non linear since we sped up",
			completed:       600,
			timestamp:       now.Add(40 * time.Second),
			expectedEta:     &fourthSampleEta,
			expectedPercent: 60.0,
			description:     "should return 24s ETA and 60% complete (rate is 15 per second)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eta, percentComplete := tracker.AddSample(tt.completed, target, tt.timestamp)
			// overly complex, see https://github.com/stretchr/testify/issues/1118
			assert.EqualValues(t, tt.expectedEta, eta,
				"%s exp=%v, got=%v", tt.description,
				reflect.Indirect(reflect.ValueOf(tt.expectedEta)),
				reflect.Indirect(reflect.ValueOf(eta)))
			assert.Equal(t, tt.expectedPercent, percentComplete, "%s exp=%v, got=%v", tt.description, tt.expectedPercent, percentComplete)
		})
	}
}
