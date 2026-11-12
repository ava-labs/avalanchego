// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp226

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	readerTests = []struct {
		name                  string
		excess                DelayExcess
		skipTestDesiredExcess bool
		delay                 uint64
	}{
		{
			name:   "zero",
			excess: 0,
			delay:  MinDelayMilliseconds,
		},
		{
			name:   "small_excess_change",
			excess: 726_820, // Smallest excess that increases the
			delay:  MinDelayMilliseconds + 1,
		},
		{
			name:                  "max_initial_excess_change",
			excess:                MaxDelayExcessDiff,
			skipTestDesiredExcess: true,
			delay:                 1,
		},
		{
			name:   "100ms_delay",
			excess: 4_828_872, // ConversionRate (2^20) * ln(100) + 2
			delay:  100,
		},
		{
			name:   "500ms_delay",
			excess: 6_516_490, // ConversionRate (2^20) * ln(500) + 2
			delay:  500,
		},
		{
			name:   "1000ms_delay",
			excess: 7_243_307, // ConversionRate (2^20) * ln(1000) + 1
			delay:  1000,
		},
		{
			name:   "2000ms_delay_initial",
			excess: InitialDelayExcess, // ConversionRate (2^20) * ln(2000) + 1
			delay:  2000,
		},
		{
			name:   "5000ms_delay",
			excess: 8_930_925, // ConversionRate (2^20) * ln(5000) + 1
			delay:  5000,
		},
		{
			name:   "10000ms_delay",
			excess: 9_657_742, // ConversionRate (2^20) * ln(10000) + 1
			delay:  10000,
		},
		{
			name:   "60000ms_delay",
			excess: 11_536_538, // ConversionRate (2^20) * ln(60000) + 1
			delay:  60000,
		},
		{
			name:   "300000ms_delay",
			excess: 13_224_156, // ConversionRate (2^20) * ln(300000) + 1
			delay:  300000,
		},
		{
			name:   "largest_int64_delay",
			excess: 45_789_502, // ConversionRate (2^20) * ln(MaxInt64)
			delay:  9_223_368_741_047_657_702,
		},
		{
			name:   "second_largest_uint64_delay",
			excess: maxDelayExcess - 1,
			delay:  18_446_728_723_565_431_225,
		},
		{
			name:   "largest_uint64_delay",
			excess: maxDelayExcess,
			delay:  math.MaxUint64,
		},
		{
			name:                  "largest_excess_delay",
			excess:                math.MaxUint64,
			skipTestDesiredExcess: true,
			delay:                 math.MaxUint64,
		},
	}
	updateExcessTests = []struct {
		name          string
		initial       DelayExcess
		desiredExcess DelayExcess
		expected      DelayExcess
	}{
		{
			name:          "no_change",
			initial:       0,
			desiredExcess: 0,
			expected:      0,
		},
		{
			name:          "max_increase",
			initial:       0,
			desiredExcess: MaxDelayExcessDiff + 1,
			expected:      MaxDelayExcessDiff, // capped
		},
		{
			name:          "inverse_max_increase",
			initial:       MaxDelayExcessDiff,
			desiredExcess: 0,
			expected:      0,
		},
		{
			name:          "max_decrease",
			initial:       2 * MaxDelayExcessDiff,
			desiredExcess: 0,
			expected:      MaxDelayExcessDiff,
		},
		{
			name:          "inverse_max_decrease",
			initial:       MaxDelayExcessDiff,
			desiredExcess: 2 * MaxDelayExcessDiff,
			expected:      2 * MaxDelayExcessDiff,
		},
		{
			name:          "small_increase",
			initial:       50,
			desiredExcess: 100,
			expected:      100,
		},
		{
			name:          "small_decrease",
			initial:       100,
			desiredExcess: 50,
			expected:      50,
		},
		{
			name:          "large_increase_capped",
			initial:       0,
			desiredExcess: 1000,
			expected:      MaxDelayExcessDiff, // capped at 200
		},
		{
			name:          "large_decrease_capped",
			initial:       1000,
			desiredExcess: 0,
			expected:      1000 - MaxDelayExcessDiff, // 800
		},
	}
)

func TestDelay(t *testing.T) {
	for _, test := range readerTests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.delay, test.excess.Delay())
		})
	}
}

func BenchmarkDelay(b *testing.B) {
	for _, test := range readerTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				test.excess.Delay()
			}
		})
	}
}

func TestUpdateDelayExcess(t *testing.T) {
	for _, test := range updateExcessTests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.initial
			initial.UpdateDelayExcess(test.desiredExcess)
			require.Equal(t, test.expected, initial)
		})
	}
}

func BenchmarkUpdateDelayExcess(b *testing.B) {
	for _, test := range updateExcessTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				initial := test.initial
				initial.UpdateDelayExcess(test.desiredExcess)
			}
		})
	}
}

func TestDesiredDelayExcess(t *testing.T) {
	for _, test := range readerTests {
		if test.skipTestDesiredExcess {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.excess, DesiredDelayExcess(test.delay))
		})
	}
}

func BenchmarkDesiredDelayExcess(b *testing.B) {
	for _, test := range readerTests {
		if test.skipTestDesiredExcess {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				DesiredDelayExcess(test.delay)
			}
		})
	}
}
