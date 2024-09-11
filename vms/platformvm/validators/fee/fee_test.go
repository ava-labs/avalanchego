// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	second = 1
	minute = 60 * second
	hour   = 60 * minute
	day    = 24 * hour
	week   = 7 * day
	year   = 365 * day

	minPrice = 2_048

	capacity                      = 20_000
	target                        = 10_000
	maxExcessIncreasePerSecond    = capacity - target
	doubleEvery                   = day
	excessIncreasePerDoubling     = maxExcessIncreasePerSecond * doubleEvery
	excessConversionConstantFloat = excessIncreasePerDoubling / math.Ln2
)

var (
	excessConversionConstant = floatToGas(excessConversionConstantFloat)

	tests = []struct {
		name   string
		state  State
		config Config

		// expectedSeconds and expectedCost are used as inputs for some tests
		// and outputs for other tests.
		expectedSeconds uint64
		expectedCost    uint64
		expectedExcess  gas.Gas
	}{
		{
			name: "excess=0, current<target, minute",
			state: State{
				Current: 10,
				Excess:  0,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: minute,
			expectedCost:    122_880,
			expectedExcess:  0, // Should not underflow
		},
		{
			name: "excess=0, current=target, minute",
			state: State{
				Current: 10_000,
				Excess:  0,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: minute,
			expectedCost:    122_880,
			expectedExcess:  0,
		},
		{
			name: "excess=excessIncreasePerDoubling, current=target, minute",
			state: State{
				Current: 10_000,
				Excess:  excessIncreasePerDoubling,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: minute,
			expectedCost:    245_760,
			expectedExcess:  excessIncreasePerDoubling,
		},
		{
			name: "excess=K, current=target, minute",
			state: State{
				Current: 10_000,
				Excess:  excessConversionConstant,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: minute,
			expectedCost:    334_020,
			expectedExcess:  excessConversionConstant,
		},
		{
			name: "excess=0, current>target, minute",
			state: State{
				Current: 15_000,
				Excess:  0,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: minute,
			expectedCost:    122_880,
			expectedExcess:  5_000 * minute,
		},
		{
			name: "excess hits 0 during, current<target, day",
			state: State{
				Current: 9_000,
				Excess:  6 * hour * 1_000,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: day,
			expectedCost:    177_321_939,
			expectedExcess:  0, // Should not underflow
		},
		{
			name: "excess=K, current=target, day",
			state: State{
				Current: 10_000,
				Excess:  excessConversionConstant,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: day,
			expectedCost:    480_988_800,
			expectedExcess:  excessConversionConstant,
		},
		{
			name: "excess=0, current>target, day",
			state: State{
				Current: 15_000,
				Excess:  0,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: day,
			expectedCost:    211_438_809,
			expectedExcess:  5_000 * day,
		},
		{
			name: "excess=0, current=target, week",
			state: State{
				Current: 10_000,
				Excess:  0,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: week,
			expectedCost:    1_238_630_400,
			expectedExcess:  0,
		},
		{
			name: "excess=0, current>target, week",
			state: State{
				Current: 15_000,
				Excess:  0,
			},
			config: Config{
				Target:                   10_000,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: week,
			expectedCost:    5_265_492_669,
			expectedExcess:  5_000 * week,
		},
		{
			name: "excess=1, current>>target, second",
			state: State{
				Current: math.MaxUint64,
				Excess:  1,
			},
			config: Config{
				Target:                   0,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: 1,
			expectedCost:    math.MaxUint64, // Should not overflow
			expectedExcess:  math.MaxUint64, // Should not overflow
		},
		{
			name: "excess=0, current>>target, 11 seconds",
			state: State{
				Current: math.MaxUint32,
				Excess:  0,
			},
			config: Config{
				Target:                   0,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: 11,
			expectedCost:    math.MaxUint64, // Should not overflow
			expectedExcess:  math.MaxUint32 * 11,
		},
	}
)

func TestStateAdvanceTime(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t,
				State{
					Current: test.state.Current,
					Excess:  test.expectedExcess,
				},
				test.state.AdvanceTime(test.config.Target, test.expectedSeconds),
			)
		})
	}
}

func TestStateCostOf(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t,
				test.expectedCost,
				test.state.CostOf(test.config, test.expectedSeconds),
			)
		})
	}
}

func TestStateSecondsUntil(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t,
				test.expectedSeconds,
				test.state.SecondsUntil(test.config, year, test.expectedCost),
			)
		})
	}
}

func BenchmarkStateCostOf(b *testing.B) {
	benchmarks := []struct {
		name   string
		costOf func(
			s State,
			c Config,
			seconds uint64,
		) uint64
	}{
		{
			name:   "unoptimized",
			costOf: State.unoptimizedCostOf,
		},
		{
			name:   "optimized",
			costOf: State.CostOf,
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			for _, benchmark := range benchmarks {
				b.Run(benchmark.name, func(b *testing.B) {
					for i := 0; i < b.N; i++ {
						benchmark.costOf(test.state, test.config, test.expectedSeconds)
					}
				})
			}
		})
	}
}

func BenchmarkStateSecondsUntil(b *testing.B) {
	benchmarks := []struct {
		name         string
		secondsUntil func(
			s State,
			c Config,
			maxSeconds uint64,
			targetCost uint64,
		) uint64
	}{
		{
			name:         "unoptimized",
			secondsUntil: State.unoptimizedSecondsUntil,
		},
		{
			name:         "optimized",
			secondsUntil: State.SecondsUntil,
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			for _, benchmark := range benchmarks {
				b.Run(benchmark.name, func(b *testing.B) {
					for i := 0; i < b.N; i++ {
						benchmark.secondsUntil(test.state, test.config, year, test.expectedCost)
					}
				})
			}
		})
	}
}

func FuzzStateCostOf(f *testing.F) {
	for _, test := range tests {
		f.Add(
			uint64(test.state.Current),
			uint64(test.state.Excess),
			uint64(test.config.Target),
			uint64(test.config.MinPrice),
			uint64(test.config.ExcessConversionConstant),
			test.expectedSeconds,
		)
	}
	f.Fuzz(
		func(
			t *testing.T,
			current uint64,
			excess uint64,
			target uint64,
			minPrice uint64,
			excessConversionConstant uint64,
			seconds uint64,
		) {
			s := State{
				Current: gas.Gas(current),
				Excess:  gas.Gas(excess),
			}
			c := Config{
				Target:                   gas.Gas(target),
				MinPrice:                 gas.Price(minPrice),
				ExcessConversionConstant: gas.Gas(max(excessConversionConstant, 1)),
			}
			seconds = min(seconds, year)
			require.Equal(
				t,
				s.unoptimizedCostOf(c, seconds),
				s.CostOf(c, seconds),
			)
		},
	)
}

func FuzzStateSecondsUntil(f *testing.F) {
	for _, test := range tests {
		f.Add(
			uint64(test.state.Current),
			uint64(test.state.Excess),
			uint64(test.config.Target),
			uint64(test.config.MinPrice),
			uint64(test.config.ExcessConversionConstant),
			uint64(year),
			test.expectedCost,
		)
	}
	f.Fuzz(
		func(
			t *testing.T,
			current uint64,
			excess uint64,
			target uint64,
			minPrice uint64,
			excessConversionConstant uint64,
			maxSeconds uint64,
			targetCost uint64,
		) {
			s := State{
				Current: gas.Gas(current),
				Excess:  gas.Gas(excess),
			}
			c := Config{
				Target:                   gas.Gas(target),
				MinPrice:                 gas.Price(minPrice),
				ExcessConversionConstant: gas.Gas(max(excessConversionConstant, 1)),
			}
			maxSeconds = min(maxSeconds, year)
			require.Equal(
				t,
				s.unoptimizedSecondsUntil(c, maxSeconds, targetCost),
				s.SecondsUntil(c, maxSeconds, targetCost),
			)
		},
	)
}

// unoptimizedCalculateCost is a naive implementation of CostOf that is used for
// differential fuzzing.
func (s State) unoptimizedCostOf(c Config, seconds uint64) uint64 {
	var (
		cost uint64
		err  error
	)
	for i := uint64(0); i < seconds; i++ {
		s = s.AdvanceTime(c.Target, 1)

		price := gas.CalculatePrice(c.MinPrice, s.Excess, c.ExcessConversionConstant)
		cost, err = safemath.Add(cost, uint64(price))
		if err != nil {
			return math.MaxUint64
		}
	}
	return cost
}

// unoptimizedSecondsUntil is a naive implementation of SecondsUntil that is
// used for differential fuzzing.
func (s State) unoptimizedSecondsUntil(c Config, maxSeconds uint64, targetCost uint64) uint64 {
	var (
		cost    uint64
		seconds uint64
		err     error
	)
	for cost < targetCost && seconds < maxSeconds {
		s = s.AdvanceTime(c.Target, 1)
		seconds++

		price := gas.CalculatePrice(c.MinPrice, s.Excess, c.ExcessConversionConstant)
		cost, err = safemath.Add(cost, uint64(price))
		if err != nil {
			return seconds
		}
	}
	return seconds
}

// floatToGas converts f to gas.Gas by truncation. `gas.Gas(f)` is preferred and
// floatToGas should only be used if its argument is a `const`.
func floatToGas(f float64) gas.Gas {
	return gas.Gas(f)
}
