// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntervalLess(t *testing.T) {
	tests := []struct {
		name     string
		left     *Interval
		right    *Interval
		expected bool
	}{
		{
			name: "less",
			left: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			right: &Interval{
				LowerBound: 11,
				UpperBound: 11,
			},
			expected: true,
		},
		{
			name: "greater",
			left: &Interval{
				LowerBound: 11,
				UpperBound: 11,
			},
			right: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			expected: false,
		},
		{
			name: "equal",
			left: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			right: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			less := test.left.Less(test.right)
			require.Equal(t, test.expected, less)
		})
	}
}

func TestIntervalContains(t *testing.T) {
	tests := []struct {
		name     string
		interval *Interval
		height   uint64
		expected bool
	}{
		{
			name:     "nil does not contain anything",
			interval: nil,
			height:   10,
			expected: false,
		},
		{
			name: "too low",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			height:   9,
			expected: false,
		},
		{
			name: "inside",
			interval: &Interval{
				LowerBound: 9,
				UpperBound: 11,
			},
			height:   10,
			expected: true,
		},
		{
			name: "equal",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			height:   10,
			expected: true,
		},
		{
			name: "too high",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			height:   11,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contains := test.interval.Contains(test.height)
			require.Equal(t, test.expected, contains)
		})
	}
}

func TestIntervalAdjacentToLowerBound(t *testing.T) {
	tests := []struct {
		name     string
		interval *Interval
		height   uint64
		expected bool
	}{
		{
			name:     "nil is not adjacent to anything",
			interval: nil,
			height:   10,
			expected: false,
		},
		{
			name: "too low",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			height:   8,
			expected: false,
		},
		{
			name: "equal",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			height:   10,
			expected: false,
		},
		{
			name: "adjacent to both",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			height:   9,
			expected: true,
		},
		{
			name: "adjacent to lower",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 11,
			},
			height:   9,
			expected: true,
		},
		{
			name: "check for overflow",
			interval: &Interval{
				LowerBound: 0,
				UpperBound: math.MaxUint64 - 1,
			},
			height:   math.MaxUint64,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adjacent := test.interval.AdjacentToLowerBound(test.height)
			require.Equal(t, test.expected, adjacent)
		})
	}
}

func TestIntervalAdjacentToUpperBound(t *testing.T) {
	tests := []struct {
		name     string
		interval *Interval
		height   uint64
		expected bool
	}{
		{
			name:     "nil is not adjacent to anything",
			interval: nil,
			height:   10,
			expected: false,
		},
		{
			name: "too low",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			height:   8,
			expected: false,
		},
		{
			name: "equal",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			height:   10,
			expected: false,
		},
		{
			name: "adjacent to both",
			interval: &Interval{
				LowerBound: 10,
				UpperBound: 10,
			},
			height:   11,
			expected: true,
		},
		{
			name: "adjacent to higher",
			interval: &Interval{
				LowerBound: 9,
				UpperBound: 10,
			},
			height:   11,
			expected: true,
		},
		{
			name: "check for overflow",
			interval: &Interval{
				LowerBound: 1,
				UpperBound: math.MaxUint64,
			},
			height:   0,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adjacent := test.interval.AdjacentToUpperBound(test.height)
			require.Equal(t, test.expected, adjacent)
		})
	}
}
