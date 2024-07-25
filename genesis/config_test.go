// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestAllocationCompare(t *testing.T) {
	type test struct {
		name     string
		alloc1   Allocation
		alloc2   Allocation
		expected int
	}
	tests := []test{
		{
			name:     "equal",
			alloc1:   Allocation{},
			alloc2:   Allocation{},
			expected: 0,
		},
		{
			name:   "initial amount smaller",
			alloc1: Allocation{},
			alloc2: Allocation{
				InitialAmount: 1,
			},
			expected: -1,
		},
		{
			name:   "bytes smaller",
			alloc1: Allocation{},
			alloc2: Allocation{
				AVAXAddr: ids.ShortID{1},
			},
			expected: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.alloc1.Compare(tt.alloc2))
			require.Equal(-tt.expected, tt.alloc2.Compare(tt.alloc1))
		})
	}
}

func TestRenewPeriod(t *testing.T) {
	localConfigStartTime := time.Unix(int64(LocalConfig.StartTime), 0)
	type test struct {
		name     string
		current  time.Time
		expected uint64
	}
	tests := []test{
		{
			name:     "1 sec before local config",
			expected: 1721016000, // Mon Jul 15 2024 04:00:00 GMT+0000
			current:  localConfigStartTime.Add(-1 * time.Second),
		},
		{
			name:     "1 sec after local config",
			expected: 1721016000, // Mon Jul 15 2024 04:00:00 GMT+0000
			current:  localConfigStartTime.Add(1 * time.Second),
		},
		{
			name:     "after less than period",
			expected: 1721016000, // Mon Jul 15 2024 04:00:00 GMT+0000
			current:  localConfigStartTime.Add(renewPeriod).Add(-1 * time.Second),
		},
		{
			name:     "after exactly 1 period",
			expected: 1721016000, // Mon Jul 15 2024 04:00:00 GMT+0000
			current:  localConfigStartTime.Add(renewPeriod),
		},
		{
			name:     "after 1 period and 1 second",
			expected: 1744344000, // Fri Apr 11 2025 04:00:00 GMT+0000
			current:  localConfigStartTime.Add(renewPeriod).Add(1 * time.Second),
		},
		{
			name:     "after 2 periods",
			expected: 1744344000, // Fri Apr 11 2025 04:00:00 GMT+0000
			current:  localConfigStartTime.Add(renewPeriod * 2),
		},
		{
			name:     "after 2 periods and 1 second",
			expected: 1767672000, // Tue Jan 06 2026 04:00:00 GMT+0000
			current:  localConfigStartTime.Add(renewPeriod * 2).Add(1 * time.Second),
		}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// to avoud modifying the global LocalConfig
			testLocalConfig := Config{
				StartTime: LocalConfig.StartTime,
			}
			testLocalConfig.renewStartTime(tt.current)
			require.Equalf(t, tt.expected, testLocalConfig.StartTime, "expected %d, got %d", tt.expected, testLocalConfig.StartTime)
		})
	}
}
