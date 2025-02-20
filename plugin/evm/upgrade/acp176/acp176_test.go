// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp176

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
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
			maxCapacity: targetToMaxCapacity * MinTargetPerSecond,
			gasPrice:    MinGasPrice,
		},
		{
			name: "almost_excess_change",
			state: State{
				Gas: gas.State{
					Excess: 29_805_331, // MinTargetPerSecond * ln(2) * TargetToPriceUpdateConversion
				},
				TargetExcess: 33, // Largest excess that doesn't increase the target
			},
			skipTestDesiredTargetExcess: true,
			target:                      MinTargetPerSecond,
			maxCapacity:                 targetToMaxCapacity * MinTargetPerSecond,
			gasPrice:                    2 * MinGasPrice,
		},
		{
			name: "small_excess_change",
			state: State{
				Gas: gas.State{
					Excess: 29_805_362, // (MinTargetPerSecond + 1) * ln(2) * TargetToPriceUpdateConversion
				},
				TargetExcess: 34, // Smallest excess that increases the target
			},
			target:      MinTargetPerSecond + 1,
			maxCapacity: targetToMaxCapacity * (MinTargetPerSecond + 1),
			gasPrice:    2 * MinGasPrice,
		},
		{
			name: "max_initial_excess_change",
			state: State{
				Gas: gas.State{
					Excess: 47_286_485, // (MinTargetPerSecond + 977) * ln(3) * TargetToPriceUpdateConversion
				},
				TargetExcess: MaxTargetExcessDiff,
			},
			skipTestDesiredTargetExcess: true,
			target:                      MinTargetPerSecond + 977,
			maxCapacity:                 targetToMaxCapacity * (MinTargetPerSecond + 977),
			gasPrice:                    3 * MinGasPrice,
		},
		{
			name: "current_target",
			state: State{
				Gas: gas.State{
					Excess: 1_336_650_647, // 1_500_000 * ln(nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			target:      1_500_000,
			maxCapacity: targetToMaxCapacity * 1_500_000,
			gasPrice:    (nAVAX + 7) * MinGasPrice, // +7 due to approximation
		},
		{
			name: "3m_target",
			state: State{
				Gas: gas.State{
					Excess: 3_267_368_247, // 3_000_000 * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 36_863_312, // 2^25 * ln(3)
			},
			target:      3_000_000,
			maxCapacity: targetToMaxCapacity * 3_000_000,
			gasPrice:    (100*nAVAX + 31) * MinGasPrice, // +31 due to approximation
		},
		{
			name: "6m_target",
			state: State{
				Gas: gas.State{
					Excess: 6_534_736_494, // 6_000_000 * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 60_121_472, // 2^25 * ln(6)
			},
			target:      6_000_000,
			maxCapacity: targetToMaxCapacity * 6_000_000,
			gasPrice:    (100*nAVAX + 31) * MinGasPrice, // +31 due to approximation
		},
		{
			name: "10m_target",
			state: State{
				Gas: gas.State{
					Excess: 10_891_227_490, // 10_000_000 * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 77_261_935, // 2^25 * ln(10)
			},
			target:      10_000_000,
			maxCapacity: targetToMaxCapacity * 10_000_000,
			gasPrice:    (100*nAVAX + 31) * MinGasPrice, // +31 due to approximation
		},
		{
			name: "100m_target",
			state: State{
				Gas: gas.State{
					Excess: 108_912_274_899, // 100_000_000 * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 154_523_870, // 2^25 * ln(100)
			},
			target:      100_000_000,
			maxCapacity: targetToMaxCapacity * 100_000_000,
			gasPrice:    (100*nAVAX + 8) * MinGasPrice, // +8 due to approximation
		},
		{
			name: "low_1b_target",
			state: State{
				Gas: gas.State{
					Excess: 1_089_122_722_848, // (1_000_000_000 - 24) * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 231_785_804, // 2^25 * ln(1000)
			},
			target:      1_000_000_000 - 24,
			maxCapacity: targetToMaxCapacity * (1_000_000_000 - 24),
			gasPrice:    (100*nAVAX + 1) * MinGasPrice, // +1 due to approximation
		},
		{
			name: "high_1b_target",
			state: State{
				Gas: gas.State{
					Excess: 1_089_122_755_521, // (1_000_000_000 + 6) * ln(100*nAVAX) * TargetToPriceUpdateConversion
				},
				TargetExcess: 231_785_805, // 2^25 * ln(1000) + 1
			},
			target:      1_000_000_000 + 6,
			maxCapacity: targetToMaxCapacity * (1_000_000_000 + 6),
			gasPrice:    (100 * nAVAX) * MinGasPrice,
		},
		{
			name: "largest_max_capacity",
			state: State{
				Gas: gas.State{
					Excess: math.MaxUint64,
				},
				TargetExcess: 924430531, // 2^25 * ln(MaxUint64 / MinMaxCapacity)
			},
			target:      922_337_190_378_117_171,
			maxCapacity: 18_446_743_807_562_343_420,
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
				TargetExcess: 1_024_950_627, // 2^25 * ln(MaxUint64 / MinTargetPerSecond) + 1
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
	advanceTimeTests = []struct {
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
					Capacity: 31_000_000, // Could happen if the targetExcess was modified
					Excess:   2_000_000,
				},
				// Set capacity to 30M
				TargetExcess: 13_605_152, // 2^25 * ln(1.5)
			},
			seconds: 0,
			expected: State{
				Gas: gas.State{
					Capacity: 30_000_000, // capped at 30M
					Excess:   2_000_000,  // unmodified
				},
				TargetExcess: 13_605_152, // unmodified
			},
		},
		{
			name: "hit_max_capacity_boundary",
			initial: State{
				Gas: gas.State{
					Capacity: 0, // Could happen if the targetExcess was modified
					Excess:   math.MaxUint64,
				},
				// Set MaxCapacity to MaxUint64
				TargetExcess: 924_430_532, // 2^25 * ln(MaxUint64 / MinMaxCapacity)
			},
			seconds: 1,
			expected: State{
				Gas: gas.State{
					Capacity: 1_844_674_435_731_815_790,                // greater than MaxUint64/10
					Excess:   math.MaxUint64 - 922_337_217_865_907_895, // MaxUint64 - capacity / TargetToMax
				},
				TargetExcess: 924_430_532, // unmodified
			},
		},
		{
			name: "hit_max_rate_boundary",
			initial: State{
				Gas: gas.State{
					Capacity: 0, // Could happen if the targetExcess was modified
					Excess:   math.MaxUint64,
				},
				// Set MaxPerSecond to MaxUint64
				TargetExcess: 1_001_692_467, // 2^25 * ln(MaxUint64 / MinMaxPerSecond)
			},
			seconds: 1,
			expected: State{
				Gas: gas.State{
					Capacity: math.MaxUint64,            // greater than MaxUint64/10
					Excess:   9_223_371_875_007_030_354, // less than MaxUint64/2
				},
				TargetExcess: 1_001_692_467, // unmodified
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

func TestAdvanceTime(t *testing.T) {
	for _, test := range advanceTimeTests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.initial
			initial.AdvanceTime(test.seconds)
			require.Equal(t, test.expected, initial)
		})
	}
}

func BenchmarkAdvanceTime(b *testing.B) {
	for _, test := range advanceTimeTests {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				initial := test.initial
				initial.AdvanceTime(test.seconds)
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
