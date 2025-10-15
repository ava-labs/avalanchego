// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp176

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

const nAVAX = 1_000_000_000

var (
	readerTests = []struct {
		name                        string
		state                       State
		skipTestDesiredTargetExcess bool
		target                      gas.Gas
		maxCapacity                 gas.Gas
		gasPrice                    gas.Price
	}{
		{
			name: "zero",
			state: State{
				Gas: gas.State{
					Excess: 0,
				},
				TargetExcess: 0,
			},
			target:      MinTargetPerSecond,
			maxCapacity: MinMaxCapacity,
			gasPrice:    MinGasPrice,
		},
		{
			name: "almost_excess_change",
			state: State{
				Gas: gas.State{
					Excess: 60_303_808, // MinTargetPerSecond * ln(2) * TargetToPriceUpdateConversion
				},
				TargetExcess: 33, // Largest excess that doesn't increase the target
			},
			skipTestDesiredTargetExcess: true,
			target:                      MinTargetPerSecond,
			maxCapacity:                 MinMaxCapacity,
			gasPrice:                    2 * MinGasPrice,
		},
		{
			name: "small_excess_change",
			state: State{
				Gas: gas.State{
					Excess: 60_303_868, // (MinTargetPerSecond + 1) * ln(2) * TargetToPriceUpdateConversion
				},
				TargetExcess: 34, // Smallest excess that increases the target
			},
			target:      MinTargetPerSecond + 1,
			maxCapacity: TargetToMaxCapacity * (MinTargetPerSecond + 1),
			gasPrice:    2 * MinGasPrice,
		},
		{
			name: "max_initial_excess_change",
			state: State{
				Gas: gas.State{
					Excess: 95_672_652, // (MinTargetPerSecond + 977) * ln(3) * TargetToPriceUpdateConversion
				},
				TargetExcess: MaxTargetExcessDiff,
			},
			skipTestDesiredTargetExcess: true,
			target:                      MinTargetPerSecond + 977,
			maxCapacity:                 TargetToMaxCapacity * (MinTargetPerSecond + 977),
			gasPrice:                    3 * MinGasPrice,
		},
		{
			name: "current_target",
			state: State{
				Gas: gas.State{
					Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			target:      1_500_000,
			maxCapacity: TargetToMaxCapacity * 1_500_000,
			gasPrice:    nAVAX*MinGasPrice + 2, // +2 due to approximation
		},
		{
			name: "3m_target",
			state: State{
				Gas: gas.State{
					Excess: 6_610_721_802, // 3_000_000 * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 36_863_312, // 2^25 * ln(3)
			},
			target:      3_000_000,
			maxCapacity: TargetToMaxCapacity * 3_000_000,
			gasPrice:    100*nAVAX*MinGasPrice + 4, // +4 due to approximation
		},
		{
			name: "6m_target",
			state: State{
				Gas: gas.State{
					Excess: 13_221_443_604, // 6_000_000 * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 60_121_472, // 2^25 * ln(6)
			},
			target:      6_000_000,
			maxCapacity: TargetToMaxCapacity * 6_000_000,
			gasPrice:    100*nAVAX*MinGasPrice + 4, // +4 due to approximation
		},
		{
			name: "10m_target",
			state: State{
				Gas: gas.State{
					Excess: 22_035_739_340, // 10_000_000 * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 77_261_935, // 2^25 * ln(10)
			},
			target:      10_000_000,
			maxCapacity: TargetToMaxCapacity * 10_000_000,
			gasPrice:    100*nAVAX*MinGasPrice + 5, // +5 due to approximation
		},
		{
			name: "100m_target",
			state: State{
				Gas: gas.State{
					Excess: 220_357_393_400, // 100_000_000 * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 154_523_870, // 2^25 * ln(100)
			},
			target:      100_000_000,
			maxCapacity: TargetToMaxCapacity * 100_000_000,
			gasPrice:    100*nAVAX*MinGasPrice + 5, // +5 due to approximation
		},
		{
			name: "low_1b_target",
			state: State{
				Gas: gas.State{
					Excess: 2_203_573_881_110, // (1_000_000_000 - 24) * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 231_785_804, // 2^25 * ln(1000)
			},
			target:      1_000_000_000 - 24,
			maxCapacity: TargetToMaxCapacity * (1_000_000_000 - 24),
			gasPrice:    100 * nAVAX * MinGasPrice,
		},
		{
			name: "high_1b_target",
			state: State{
				Gas: gas.State{
					Excess: 2_203_573_947_217, // (1_000_000_000 + 6) * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 231_785_805, // 2^25 * ln(1000) + 1
			},
			target:      1_000_000_000 + 6,
			maxCapacity: TargetToMaxCapacity * (1_000_000_000 + 6),
			gasPrice:    100 * nAVAX * MinGasPrice,
		},
		{
			name: "largest_max_capacity",
			state: State{
				Gas: gas.State{
					Excess: math.MaxUint64,
				},
				TargetExcess: 947_688_691, // 2^25 * ln(MaxUint64 / MinMaxCapacity)
			},
			target:      1_844_674_384_269_701_322,
			maxCapacity: 18_446_743_842_697_013_220,
			gasPrice:    2 * MinGasPrice,
		},
		{
			name: "largest_int64_target",
			state: State{
				Gas: gas.State{
					Excess: math.MaxUint64,
				},
				TargetExcess: 1_001_692_466, // 2^25 * ln(MaxInt64 / MinTargetPerSecond)
			},
			target:      9_223_371_923_824_614_091,
			maxCapacity: math.MaxUint64,
			gasPrice:    2 * MinGasPrice,
		},
		{
			name: "second_largest_uint64_target",
			state: State{
				Gas: gas.State{
					Excess: math.MaxUint64,
				},
				TargetExcess: 1_024_950_626, // 2^25 * ln(MaxUint64 / MinTargetPerSecond)
			},
			target:      18_446_743_882_783_898_031,
			maxCapacity: math.MaxUint64,
			gasPrice:    2 * MinGasPrice,
		},
		{
			name: "largest_uint64_target",
			state: State{
				Gas: gas.State{
					Excess: math.MaxUint64,
				},
				TargetExcess: maxTargetExcess,
			},
			target:      math.MaxUint64,
			maxCapacity: math.MaxUint64,
			gasPrice:    2 * MinGasPrice,
		},
		{
			name: "largest_excess",
			state: State{
				Gas: gas.State{
					Excess: math.MaxUint64,
				},
				TargetExcess: math.MaxUint64,
			},
			skipTestDesiredTargetExcess: true,
			target:                      math.MaxUint64,
			maxCapacity:                 math.MaxUint64,
			gasPrice:                    2 * MinGasPrice,
		},
	}
	advanceSecondsTests = []struct {
		name     string
		initial  State
		seconds  uint64
		expected State
	}{
		{
			name: "0_seconds",
			initial: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   2_000_000,
				},
				// Set target to 1.5M per second
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			seconds: 0,
			expected: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   2_000_000,
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "1_seconds",
			initial: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   2_000_000,
				},
				// Set target to 1.5M per second
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			seconds: 1,
			expected: State{
				Gas: gas.State{
					Capacity: 3_000_000,
					Excess:   500_000,
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "5_seconds",
			initial: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   15_000_000,
				},
				// Set target to 1.5M per second
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			seconds: 5,
			expected: State{
				Gas: gas.State{
					Capacity: 15_000_000,
					Excess:   7_500_000,
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "0_seconds_over_capacity",
			initial: State{
				Gas: gas.State{
					Capacity: 16_000_000, // Could happen if the targetExcess was modified
					Excess:   2_000_000,
				},
				// Set capacity to 15M
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			seconds: 0,
			expected: State{
				Gas: gas.State{
					Capacity: 15_000_000, // capped at 15M
					Excess:   2_000_000,  // unmodified
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "max_capacity_overflow",
			initial: State{
				Gas: gas.State{
					Capacity: 0, // Could happen if the targetExcess was modified
					Excess:   math.MaxUint64,
				},
				// Set MaxCapacity to MaxUint64
				// Requires Target >= MaxUint64 / TargetToMaxCapacity
				TargetExcess: 947_688_692, // ceil(TargetConversion * ln(MaxUint64 / MinMaxCapacity))
			},
			seconds: 1,
			expected: State{
				Gas: gas.State{
					Capacity: 3_689_348_878_490_565_692,                        // greater than MaxUint / TargetToMaxCapacity
					Excess:   math.MaxUint64 - (3_689_348_878_490_565_692 / 2), // MaxUint64 - capacity / TargetToMax
				},
				TargetExcess: 947_688_692, // unmodified
			},
		},
		{
			name: "max_rate_overflow",
			initial: State{
				Gas: gas.State{
					Capacity: 0, // Could happen if the targetExcess was modified
					Excess:   math.MaxUint64,
				},
				// Set MaxPerSecond to MaxUint64
				TargetExcess: 1_001_692_467, // ceil(TargetConversion * ln(MaxUint64 / MinMaxPerSecond))
			},
			seconds: 1,
			expected: State{
				Gas: gas.State{
					Capacity: math.MaxUint64,            // greater than MaxUint / TargetToMaxCapacity
					Excess:   9_223_371_875_007_030_354, // less than MaxUint / TargetToMax
				},
				TargetExcess: 1_001_692_467, // unmodified
			},
		},
	}
	advanceMillisecondsTests = []struct {
		name         string
		initial      State
		milliseconds uint64
		expected     State
	}{
		{
			name: "0_ms",
			initial: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   2_000_000,
				},
				// Set target to 1.5M per second
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			milliseconds: 0,
			expected: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   2_000_000,
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "1000_ms",
			initial: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   2_000_000,
				},
				// Set target to 1.5M per second
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			milliseconds: 1000,
			expected: State{
				Gas: gas.State{
					Capacity: 3_000_000,
					Excess:   500_000,
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "5000_ms",
			initial: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   15_000_000,
				},
				// Set target to 1.5M per second
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			milliseconds: 5000,
			expected: State{
				Gas: gas.State{
					Capacity: 15_000_000,
					Excess:   7_500_000,
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "0_ms_over_capacity",
			initial: State{
				Gas: gas.State{
					Capacity: 16_000_000, // Could happen if the targetExcess was modified
					Excess:   2_000_000,
				},
				// Set capacity to 15M
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			milliseconds: 0,
			expected: State{
				Gas: gas.State{
					Capacity: 15_000_000, // capped at 15M
					Excess:   2_000_000,  // unmodified
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "max_capacity_overflow",
			initial: State{
				Gas: gas.State{
					Capacity: 0, // Could happen if the targetExcess was modified
					Excess:   math.MaxUint64,
				},
				// Set MaxCapacity to MaxUint64
				// Requires Target >= MaxUint64 / TargetToMaxCapacity
				// Target in seconds is used for calculating maxCapacity
				TargetExcess: 947_688_692, // ceil(TargetConversion * ln(MaxUint64 / MinMaxCapacity))
			},
			milliseconds: 1000,
			expected: State{
				Gas: gas.State{
					Capacity: 3_689_348_878_490_564_000,                        // greater than MaxUint64 / TargetToMaxCapacity
					Excess:   math.MaxUint64 - (3_689_348_878_490_564_000 / 2), // MaxUint64 - capacity / TargetToMax
				},
				TargetExcess: 947_688_692, // unmodified
			},
		},
		{
			name: "max_rate_overflow",
			initial: State{
				Gas: gas.State{
					Capacity: 0, // Could happen if the targetExcess was modified
					Excess:   math.MaxUint64,
				},
				// Set MaxPerSecond to MaxUint64
				TargetExcess: 1_001_692_467, // 2^25 * ln(MaxUint64 / MinMaxPerSecond)
			},
			milliseconds: 1000,
			expected: State{
				Gas: gas.State{
					Capacity: math.MaxUint64,            // greater than MaxUint64 / TargetToMaxCapacity
					Excess:   9_223_371_875_007_030_615, // less than MaxUint / TargetToMax
				},
				TargetExcess: 1_001_692_467, // unmodified
			},
		},
		{
			name: "1_millisecond",
			initial: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   2_000_000,
				},
				// Set target to 1.5M per second
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			milliseconds: 1,
			expected: State{
				Gas: gas.State{
					Capacity: 3_000,
					Excess:   1_998_500,
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "target_rounds_down",
			initial: State{
				Gas: gas.State{
					Capacity: 0,
					Excess:   2_000_000,
				},
				// Set target to 1_500_999 per second
				TargetExcess: 13_627_491, // 2^25 * ln(1.500999)
			},
			milliseconds: 999, // large to ensure target is rounding, not result.
			expected: State{
				Gas: gas.State{
					Capacity: 2_997_000, // 3_000 * 999
					Excess:   501_500,   // 2_000_000 - 1_500 * 999
				},
				TargetExcess: 13_627_491, // unmodified
			},
		},
	}
	consumeGasTests = []struct {
		name         string
		initial      State
		gasUsed      uint64
		extraGasUsed *big.Int
		expectedErr  error
		expected     State
	}{
		{
			name: "no_gas_used",
			initial: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
			gasUsed:      0,
			extraGasUsed: nil,
			expected: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
		},
		{
			name: "some_gas_used",
			initial: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
			gasUsed:      100_000,
			extraGasUsed: nil,
			expected: State{
				Gas: gas.State{
					Capacity: 900_000,
					Excess:   2_100_000,
				},
			},
		},
		{
			name: "some_extra_gas_used",
			initial: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
			gasUsed:      0,
			extraGasUsed: big.NewInt(100_000),
			expected: State{
				Gas: gas.State{
					Capacity: 900_000,
					Excess:   2_100_000,
				},
			},
		},
		{
			name: "both_gas_used",
			initial: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
			gasUsed:      10_000,
			extraGasUsed: big.NewInt(100_000),
			expected: State{
				Gas: gas.State{
					Capacity: 890_000,
					Excess:   2_110_000,
				},
			},
		},
		{
			name: "gas_used_capacity_exceeded",
			initial: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
			gasUsed:      1_000_001,
			extraGasUsed: nil,
			expectedErr:  gas.ErrInsufficientCapacity,
			expected: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
		},
		{
			name: "massive_extra_gas_used",
			initial: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
			gasUsed:      0,
			extraGasUsed: new(big.Int).Lsh(common.Big1, 64),
			expectedErr:  gas.ErrInsufficientCapacity,
			expected: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
		},
		{
			name: "extra_gas_used_capacity_exceeded",
			initial: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
			gasUsed:      0,
			extraGasUsed: big.NewInt(1_000_001),
			expectedErr:  gas.ErrInsufficientCapacity,
			expected: State{
				Gas: gas.State{
					Capacity: 1_000_000,
					Excess:   2_000_000,
				},
			},
		},
	}
	updateTargetExcessTests = []struct {
		name                string
		initial             State
		desiredTargetExcess gas.Gas
		expected            State
	}{
		{
			name: "no_change",
			initial: State{
				Gas: gas.State{
					Excess: 2_000_000,
				},
				TargetExcess: 0,
			},
			desiredTargetExcess: 0,
			expected: State{
				Gas: gas.State{
					Excess: 2_000_000,
				},
				TargetExcess: 0,
			},
		},
		{
			name: "max_increase",
			initial: State{
				Gas: gas.State{
					Excess: 2_000_000,
				},
				TargetExcess: 0,
			},
			desiredTargetExcess: MaxTargetExcessDiff + 1,
			expected: State{
				Gas: gas.State{
					Excess: 2_001_954, // 2M * NewTarget / OldTarget
				},
				TargetExcess: MaxTargetExcessDiff, // capped
			},
		},
		{
			name: "inverse_max_increase",
			initial: State{
				Gas: gas.State{
					Excess: 2_001_954,
				},
				TargetExcess: MaxTargetExcessDiff,
			},
			desiredTargetExcess: 0,
			expected: State{
				Gas: gas.State{
					Excess: 2_000_000, // inverse of max_increase
				},
				TargetExcess: 0,
			},
		},
		{
			name: "max_decrease",
			initial: State{
				Gas: gas.State{
					Excess: 2_000_000_000,
				},
				TargetExcess: 2 * MaxTargetExcessDiff,
			},
			desiredTargetExcess: 0,
			expected: State{
				Gas: gas.State{
					Excess: 1_998_047_816, // 2M * NewTarget / OldTarget
				},
				TargetExcess: MaxTargetExcessDiff,
			},
		},
		{
			name: "inverse_max_decrease",
			initial: State{
				Gas: gas.State{
					Excess: 1_998_047_816,
				},
				TargetExcess: MaxTargetExcessDiff,
			},
			desiredTargetExcess: 2 * MaxTargetExcessDiff,
			expected: State{
				Gas: gas.State{
					Excess: 1_999_999_999, // inverse of max_decrease -1 due to rounding error
				},
				TargetExcess: 2 * MaxTargetExcessDiff,
			},
		},
		{
			name: "reduce_capacity",
			initial: State{
				Gas: gas.State{
					Capacity: 10_019_550, // MinMaxCapacity * e^(2*MaxTargetExcessDiff / TargetConversion)
					Excess:   2_000_000_000,
				},
				TargetExcess: 2 * MaxTargetExcessDiff,
			},
			desiredTargetExcess: 0,
			expected: State{
				Gas: gas.State{
					Capacity: 10_009_770,    // MinMaxCapacity * e^(MaxTargetExcessDiff / TargetConversion)
					Excess:   1_998_047_816, // 2M * NewTarget / OldTarget
				},
				TargetExcess: MaxTargetExcessDiff,
			},
		},
		{
			name: "overflow_max_capacity",
			initial: State{
				Gas: gas.State{
					Capacity: math.MaxUint64,
					Excess:   2_000_000_000,
				},
				TargetExcess: maxTargetExcess,
			},
			desiredTargetExcess: 0,
			expected: State{
				Gas: gas.State{
					Capacity: math.MaxUint64,
					Excess:   1_998_047_867, // 2M * NewTarget / OldTarget
				},
				TargetExcess: maxTargetExcess - MaxTargetExcessDiff,
			},
		},
		{
			name: "overflow_excess",
			initial: State{
				Gas: gas.State{
					Capacity: math.MaxUint64,
					Excess:   math.MaxUint64,
				},
				TargetExcess: maxTargetExcess - MaxTargetExcessDiff,
			},
			desiredTargetExcess: maxTargetExcess,
			expected: State{
				Gas: gas.State{
					Capacity: math.MaxUint64,
					Excess:   math.MaxUint64,
				},
				TargetExcess: maxTargetExcess,
			},
		},
	}
	parseTests = []struct {
		name        string
		bytes       []byte
		state       State
		expectedErr error
	}{
		{
			name:        "insufficient_length",
			bytes:       make([]byte, StateSize-1),
			expectedErr: ErrStateInsufficientLength,
		},
		{
			name:  "zero_state",
			bytes: make([]byte, StateSize),
			state: State{},
		},
		{
			name: "truncate_bytes",
			bytes: []byte{
				StateSize: 1,
			},
			state: State{},
		},
		{
			name: "endianess",
			bytes: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
			},
			state: State{
				Gas: gas.State{
					Capacity: 0x0102030405060708,
					Excess:   0x1112131415161718,
				},
				TargetExcess: 0x2122232425262728,
			},
		},
	}
)

func TestTarget(t *testing.T) {
	for _, test := range readerTests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.target, test.state.Target())
		})
	}
}

func BenchmarkTarget(b *testing.B) {
	for _, test := range readerTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				test.state.Target()
			}
		})
	}
}

func TestMaxCapacity(t *testing.T) {
	for _, test := range readerTests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.maxCapacity, test.state.MaxCapacity())
		})
	}
}

func BenchmarkMaxCapacity(b *testing.B) {
	for _, test := range readerTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				test.state.MaxCapacity()
			}
		})
	}
}

func TestGasPrice(t *testing.T) {
	for _, test := range readerTests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.gasPrice, test.state.GasPrice())
		})
	}
}

func BenchmarkGasPrice(b *testing.B) {
	for _, test := range readerTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				test.state.GasPrice()
			}
		})
	}
}

func TestAdvanceSeconds(t *testing.T) {
	for _, test := range advanceSecondsTests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.initial
			initial.AdvanceSeconds(test.seconds)
			require.Equal(t, test.expected, initial)
		})
	}
}

func BenchmarkAdvanceSeconds(b *testing.B) {
	for _, test := range advanceSecondsTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				initial := test.initial
				initial.AdvanceSeconds(test.seconds)
			}
		})
	}
}

func TestAdvanceMilliseconds(t *testing.T) {
	for _, test := range advanceMillisecondsTests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.initial
			initial.AdvanceMilliseconds(test.milliseconds)
			require.Equal(t, test.expected, initial)
		})
	}
}

func BenchmarkAdvanceMilliseconds(b *testing.B) {
	for _, test := range advanceMillisecondsTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				initial := test.initial
				initial.AdvanceMilliseconds(test.milliseconds)
			}
		})
	}
}

func TestConsumeGas(t *testing.T) {
	for _, test := range consumeGasTests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.initial
			err := initial.ConsumeGas(test.gasUsed, test.extraGasUsed)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expected, initial)
		})
	}
}

func BenchmarkConsumeGas(b *testing.B) {
	for _, test := range consumeGasTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				initial := test.initial
				_ = initial.ConsumeGas(test.gasUsed, test.extraGasUsed)
			}
		})
	}
}

func TestUpdateTargetExcess(t *testing.T) {
	for _, test := range updateTargetExcessTests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.initial
			initial.UpdateTargetExcess(test.desiredTargetExcess)
			require.Equal(t, test.expected, initial)
		})
	}
}

func BenchmarkUpdateTargetExcess(b *testing.B) {
	for _, test := range updateTargetExcessTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				initial := test.initial
				initial.UpdateTargetExcess(test.desiredTargetExcess)
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
			require.Equal(t, test.state.TargetExcess, DesiredTargetExcess(test.target))
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
				DesiredTargetExcess(test.target)
			}
		})
	}
}

func TestParseState(t *testing.T) {
	for _, test := range parseTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			state, err := ParseState(test.bytes)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.state, state)
		})
	}
}

func BenchmarkParseState(b *testing.B) {
	for _, test := range parseTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				_, _ = ParseState(test.bytes)
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
			bytes := test.state.Bytes()
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
				_ = test.state.Bytes()
			}
		})
	}
}
