// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp226

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	readerTests = []struct {
		name                        string
		excess                      DelayExcess
		skipTestDesiredTargetExcess bool
		delay                       uint64
	}{
		{
			name:   "zero",
			excess: 0,
			delay:  MinDelayMilliseconds,
		},
		{
			name:   "small_excess_change",
			excess: 726_820, // Smallest excess that increases the target
			delay:  MinDelayMilliseconds + 1,
		},
		{
			name:                        "max_initial_excess_change",
			excess:                      MaxDelayExcessDiff,
			skipTestDesiredTargetExcess: true,
			delay:                       1,
		},
		{
			name:   "100ms_target",
			excess: 4_828_872, // ConversionRate (2^20) * ln(100) + 2
			delay:  100,
		},
		{
			name:   "500ms_target",
			excess: 6_516_490, // ConversionRate (2^20) * ln(500) + 2
			delay:  500,
		},
		{
			name:   "1000ms_target",
			excess: 7_243_307, // ConversionRate (2^20) * ln(1000) + 1
			delay:  1000,
		},
		{
			name:   "2000ms_target",
			excess: 7_970_124, // ConversionRate (2^20) * ln(2000) + 1
			delay:  2000,
		},
		{
			name:   "5000ms_target",
			excess: 8_930_925, // ConversionRate (2^20) * ln(5000) + 1
			delay:  5000,
		},
		{
			name:   "10000ms_target",
			excess: 9_657_742, // ConversionRate (2^20) * ln(10000) + 1
			delay:  10000,
		},
		{
			name:   "60000ms_target",
			excess: 11_536_538, // ConversionRate (2^20) * ln(60000) + 1
			delay:  60000,
		},
		{
			name:   "300000ms_target",
			excess: 13_224_156, // ConversionRate (2^20) * ln(300000) + 1
			delay:  300000,
		},
		{
			name:   "largest_int64_target",
			excess: 45_789_502, // ConversionRate (2^20) * ln(MaxInt64)
			delay:  9_223_368_741_047_657_702,
		},
		{
			name:   "second_largest_uint64_target",
			excess: maxDelayExcess - 1,
			delay:  18_446_728_723_565_431_225,
		},
		{
			name:   "largest_uint64_target",
			excess: maxDelayExcess,
			delay:  math.MaxUint64,
		},
		{
			name:                        "largest_excess",
			excess:                      math.MaxUint64,
			skipTestDesiredTargetExcess: true,
			delay:                       math.MaxUint64,
		},
	}
	updateTargetExcessTests = []struct {
		name                string
		initial             DelayExcess
		desiredTargetExcess uint64
		expected            DelayExcess
	}{
		{
			name:                "no_change",
			initial:             0,
			desiredTargetExcess: 0,
			expected:            0,
		},
		{
			name:                "max_increase",
			initial:             0,
			desiredTargetExcess: MaxDelayExcessDiff + 1,
			expected:            MaxDelayExcessDiff, // capped
		},
		{
			name:                "inverse_max_increase",
			initial:             MaxDelayExcessDiff,
			desiredTargetExcess: 0,
			expected:            0,
		},
		{
			name:                "max_decrease",
			initial:             2 * MaxDelayExcessDiff,
			desiredTargetExcess: 0,
			expected:            MaxDelayExcessDiff,
		},
		{
			name:                "inverse_max_decrease",
			initial:             MaxDelayExcessDiff,
			desiredTargetExcess: 2 * MaxDelayExcessDiff,
			expected:            2 * MaxDelayExcessDiff,
		},
		{
			name:                "small_increase",
			initial:             50,
			desiredTargetExcess: 100,
			expected:            100,
		},
		{
			name:                "small_decrease",
			initial:             100,
			desiredTargetExcess: 50,
			expected:            50,
		},
		{
			name:                "large_increase_capped",
			initial:             0,
			desiredTargetExcess: 1000,
			expected:            MaxDelayExcessDiff, // capped at 200
		},
		{
			name:                "large_decrease_capped",
			initial:             1000,
			desiredTargetExcess: 0,
			expected:            1000 - MaxDelayExcessDiff, // 800
		},
	}
)

func TestTargetDelay(t *testing.T) {
	for _, test := range readerTests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.delay, test.excess.Delay())
		})
	}
}

func BenchmarkTargetDelay(b *testing.B) {
	for _, test := range readerTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				test.excess.Delay()
			}
		})
	}
}

func TestUpdateTargetExcess(t *testing.T) {
	for _, test := range updateTargetExcessTests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.initial
			initial.UpdateDelayExcess(test.desiredTargetExcess)
			require.Equal(t, test.expected, initial)
		})
	}
}

func BenchmarkUpdateTargetExcess(b *testing.B) {
	for _, test := range updateTargetExcessTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				initial := test.initial
				initial.UpdateDelayExcess(test.desiredTargetExcess)
			}
		})
	}
}

func TestDesiredTargetExcess(t *testing.T) {
	for _, test := range readerTests {
		if test.skipTestDesiredTargetExcess {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.excess, DelayExcess(DesiredDelayExcess(test.delay)))
		})
	}
}

func BenchmarkDesiredTargetExcess(b *testing.B) {
	for _, test := range readerTests {
		if test.skipTestDesiredTargetExcess {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				DesiredDelayExcess(test.delay)
			}
		})
	}
}
