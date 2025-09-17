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
		excess                      TargetDelayExcess
		skipTestDesiredTargetExcess bool
		delay                       uint64
	}{
		{
			name:   "zero",
			excess: 0,
			delay:  MinTargetDelayMilliseconds,
		},
		{
			name:   "small_excess_change",
			excess: 726_820, // Smallest excess that increases the target
			delay:  MinTargetDelayMilliseconds + 1,
		},
		{
			name:                        "max_initial_excess_change",
			excess:                      MaxTargetDelayExcessDiff,
			skipTestDesiredTargetExcess: true,
			delay:                       1,
		},
		{
			name:   "100ms_target",
			excess: 4_828_872, // TargetConversion (2^20) * ln(100) + 2
			delay:  100,
		},
		{
			name:   "500ms_target",
			excess: 6_516_490, // TargetConversion (2^20) * ln(500) + 2
			delay:  500,
		},
		{
			name:   "1000ms_target",
			excess: 7_243_307, // TargetConversion (2^20) * ln(1000) + 1
			delay:  1000,
		},
		{
			name:   "2000ms_target",
			excess: 7_970_124, // TargetConversion (2^20) * ln(2000) + 1
			delay:  2000,
		},
		{
			name:   "5000ms_target",
			excess: 8_930_925, // TargetConversion (2^20) * ln(5000) + 1
			delay:  5000,
		},
		{
			name:   "10000ms_target",
			excess: 9_657_742, // TargetConversion (2^20) * ln(10000) + 1
			delay:  10000,
		},
		{
			name:   "60000ms_target",
			excess: 11_536_538, // TargetConversion (2^20) * ln(60000) + 1
			delay:  60000,
		},
		{
			name:   "300000ms_target",
			excess: 13_224_156, // TargetConversion (2^20) * ln(300000) + 1
			delay:  300000,
		},
		{
			name:   "largest_int64_target",
			excess: 45_789_502, // TargetConversion (2^20) * ln(MaxInt64)
			delay:  9_223_368_741_047_657_702,
		},
		{
			name:   "second_largest_uint64_target",
			excess: maxTargetDelayExcess - 1,
			delay:  18_446_728_723_565_431_225,
		},
		{
			name:   "largest_uint64_target",
			excess: maxTargetDelayExcess,
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
		initial             TargetDelayExcess
		desiredTargetExcess uint64
		expected            TargetDelayExcess
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
			desiredTargetExcess: MaxTargetDelayExcessDiff + 1,
			expected:            MaxTargetDelayExcessDiff, // capped
		},
		{
			name:                "inverse_max_increase",
			initial:             MaxTargetDelayExcessDiff,
			desiredTargetExcess: 0,
			expected:            0,
		},
		{
			name:                "max_decrease",
			initial:             2 * MaxTargetDelayExcessDiff,
			desiredTargetExcess: 0,
			expected:            MaxTargetDelayExcessDiff,
		},
		{
			name:                "inverse_max_decrease",
			initial:             MaxTargetDelayExcessDiff,
			desiredTargetExcess: 2 * MaxTargetDelayExcessDiff,
			expected:            2 * MaxTargetDelayExcessDiff,
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
			expected:            MaxTargetDelayExcessDiff, // capped at 200
		},
		{
			name:                "large_decrease_capped",
			initial:             1000,
			desiredTargetExcess: 0,
			expected:            1000 - MaxTargetDelayExcessDiff, // 800
		},
	}
	parseTests = []struct {
		name        string
		bytes       []byte
		excess      TargetDelayExcess
		expectedErr error
	}{
		{
			name:        "insufficient_length",
			bytes:       make([]byte, StateSize-1),
			expectedErr: ErrStateInsufficientLength,
		},
		{
			name:   "zero_state",
			bytes:  make([]byte, StateSize),
			excess: 0,
		},
		{
			name:   "truncate_bytes",
			bytes:  []byte{StateSize: 1},
			excess: 0,
		},
		{
			name: "endianness",
			bytes: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			},
			excess: 0x0102030405060708,
		},
		{
			name: "max_uint64",
			bytes: []byte{
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			},
			excess: math.MaxUint64,
		},
		{
			name: "min_uint64",
			bytes: []byte{
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			excess: 0,
		},
	}
)

func TestTargetDelay(t *testing.T) {
	for _, test := range readerTests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.delay, test.excess.TargetDelay())
		})
	}
}

func BenchmarkTargetDelay(b *testing.B) {
	for _, test := range readerTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				test.excess.TargetDelay()
			}
		})
	}
}

func TestUpdateTargetExcess(t *testing.T) {
	for _, test := range updateTargetExcessTests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.initial
			initial.UpdateTargetDelayExcess(test.desiredTargetExcess)
			require.Equal(t, test.expected, initial)
		})
	}
}

func BenchmarkUpdateTargetExcess(b *testing.B) {
	for _, test := range updateTargetExcessTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				initial := test.initial
				initial.UpdateTargetDelayExcess(test.desiredTargetExcess)
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
			require.Equal(t, test.excess, TargetDelayExcess(DesiredTargetDelayExcess(test.delay)))
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
				DesiredTargetDelayExcess(test.delay)
			}
		})
	}
}

func TestParseMinimumBlockDelayExcess(t *testing.T) {
	for _, test := range parseTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			excess, err := ParseTargetDelayExcess(test.bytes)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.excess, excess)
		})
	}
}

func BenchmarkParseMinimumBlockDelayExcess(b *testing.B) {
	for _, test := range parseTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				_, _ = ParseTargetDelayExcess(test.bytes)
			}
		})
	}
}

func TestBytes(t *testing.T) {
	for _, test := range parseTests {
		if test.expectedErr != nil {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			expectedBytes := test.bytes[:StateSize]
			bytes := test.excess.Bytes()
			require.Equal(t, expectedBytes, bytes)
		})
	}
}

func BenchmarkBytes(b *testing.B) {
	for _, test := range parseTests {
		if test.expectedErr != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				_ = test.excess.Bytes()
			}
		})
	}
}
