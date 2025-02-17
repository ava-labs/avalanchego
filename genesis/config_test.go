// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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

func TestGetRecentStartTime(t *testing.T) {
	type test struct {
		name     string
		defined  time.Time
		now      time.Time
		expected time.Time
	}
	tests := []test{
		{
			name:     "before 1 period and 1 second",
			defined:  time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
			now:      time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(-localNetworkUpdateStartTimePeriod - time.Second),
			expected: time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
		},
		{
			name:     "before 1 second",
			defined:  time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
			now:      time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(-time.Second),
			expected: time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
		},
		{
			name:     "equal",
			defined:  time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
			now:      time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
			expected: time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
		},
		{
			name:     "after 1 second",
			defined:  time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
			now:      time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(time.Second),
			expected: time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
		},
		{
			name:     "after 1 period",
			defined:  time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
			now:      time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(localNetworkUpdateStartTimePeriod),
			expected: time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(localNetworkUpdateStartTimePeriod),
		},
		{
			name:     "after 1 period and 1 second",
			defined:  time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
			now:      time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(localNetworkUpdateStartTimePeriod + time.Second),
			expected: time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(localNetworkUpdateStartTimePeriod),
		},
		{
			name:     "after 2 periods",
			defined:  time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
			now:      time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(2 * localNetworkUpdateStartTimePeriod),
			expected: time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(2 * localNetworkUpdateStartTimePeriod),
		},
		{
			name:     "after 2 periods and 1 second",
			defined:  time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC),
			now:      time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(2*localNetworkUpdateStartTimePeriod + time.Second),
			expected: time.Date(2024, time.July, 15, 4, 0, 0, 0, time.UTC).Add(2 * localNetworkUpdateStartTimePeriod),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := getRecentStartTime(tt.defined, tt.now, localNetworkUpdateStartTimePeriod)
			require.Equal(t, tt.expected, actual)
		})
	}
}
