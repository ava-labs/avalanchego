// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
			name: "excess=0, current>>target, 10 seconds",
			state: State{
				Current: math.MaxUint32,
				Excess:  0,
			},
			config: Config{
				Target:                   0,
				MinPrice:                 minPrice,
				ExcessConversionConstant: excessConversionConstant,
			},
			expectedSeconds: 10,
			expectedCost:    1_948_429_840_780_833_612,
			expectedExcess:  math.MaxUint32 * 10,
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

func TestStateCostOfOverflow(t *testing.T) {
	const target = 10_000
	config := Config{
		Target:                   target,
		MinPrice:                 minPrice,
		ExcessConversionConstant: excessConversionConstant,
	}

	tests := []struct {
		name    string
		state   State
		seconds uint64
	}{
		{
			name: "current > target",
			state: State{
				Current: math.MaxUint32,
				Excess:  0,
			},
			seconds: math.MaxUint64,
		},
		{
			name: "current == target",
			state: State{
				Current: target,
				Excess:  0,
			},
			seconds: math.MaxUint64,
		},
		{
			name: "current < target",
			state: State{
				Current: 0,
				Excess:  0,
			},
			seconds: math.MaxUint64,
		},
		{
			name: "current < target and reasonable excess",
			state: State{
				Current: 0,
				Excess:  target + 1,
			},
			seconds: math.MaxUint64/minPrice + 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t,
				uint64(math.MaxUint64), // Should not overflow
				test.state.CostOf(config, test.seconds),
			)
		})
	}
}

func TestStateSecondsRemaining(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t,
				test.expectedSeconds,
				test.state.SecondsRemaining(test.config, week, test.expectedCost),
			)
		})
	}
}

func TestStateSecondsRemainingLimit(t *testing.T) {
	const target = 10_000
	tests := []struct {
		name      string
		state     State
		minPrice  gas.Price
		costLimit uint64
	}{
		{
			name: "zero price",
			state: State{
				Current: math.MaxUint32,
				Excess:  0,
			},
			minPrice:  0,
			costLimit: 0,
		},
		{
			name: "current > target",
			state: State{
				Current: target + 1,
				Excess:  0,
			},
			minPrice:  minPrice,
			costLimit: math.MaxUint64,
		},
		{
			name: "current == target",
			state: State{
				Current: target,
				Excess:  0,
			},
			minPrice:  minPrice,
			costLimit: minPrice * (week + 1),
		},
		{
			name: "current < target",
			state: State{
				Current: 0,
				Excess:  0,
			},
			minPrice:  minPrice,
			costLimit: minPrice * (week + 1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Config{
				Target:                   target,
				MinPrice:                 test.minPrice,
				ExcessConversionConstant: excessConversionConstant,
			}
			require.Equal(
				t,
				uint64(week),
				test.state.SecondsRemaining(config, week, test.costLimit),
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

func BenchmarkStateSecondsRemaining(b *testing.B) {
	benchmarks := []struct {
		name             string
		secondsRemaining func(
			s State,
			c Config,
			maxSeconds uint64,
			targetCost uint64,
		) uint64
	}{
		{
			name:             "unoptimized",
			secondsRemaining: State.unoptimizedSecondsRemaining,
		},
		{
			name:             "optimized",
			secondsRemaining: State.SecondsRemaining,
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			for _, benchmark := range benchmarks {
				b.Run(benchmark.name, func(b *testing.B) {
					for i := 0; i < b.N; i++ {
						benchmark.secondsRemaining(test.state, test.config, week, test.expectedCost)
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
			seconds = min(seconds, hour)
			require.Equal(
				t,
				s.unoptimizedCostOf(c, seconds),
				s.CostOf(c, seconds),
			)
		},
	)
}

func FuzzStateSecondsRemaining(f *testing.F) {
	for _, test := range tests {
		f.Add(
			uint64(test.state.Current),
			uint64(test.state.Excess),
			uint64(test.config.Target),
			uint64(test.config.MinPrice),
			uint64(test.config.ExcessConversionConstant),
			uint64(hour),
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
			maxSeconds = min(maxSeconds, hour)
			require.Equal(
				t,
				s.unoptimizedSecondsRemaining(c, maxSeconds, targetCost),
				s.SecondsRemaining(c, maxSeconds, targetCost),
			)
		},
	)
}

// unoptimizedCostOf is a naive implementation of CostOf that is used for
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

// unoptimizedSecondsRemaining is a naive implementation of SecondsRemaining
// that is used for differential fuzzing.
func (s State) unoptimizedSecondsRemaining(c Config, maxSeconds uint64, fundsRemaining uint64) uint64 {
	for seconds := uint64(0); seconds < maxSeconds; seconds++ {
		s = s.AdvanceTime(c.Target, 1)

		price := uint64(gas.CalculatePrice(c.MinPrice, s.Excess, c.ExcessConversionConstant))
		if price > fundsRemaining {
			return seconds
		}
		fundsRemaining -= price
	}
	return maxSeconds
}

// floatToGas converts f to gas.Gas by truncation. `gas.Gas(f)` is preferred and
// floatToGas should only be used if its argument is a `const`.
func floatToGas(f float64) gas.Gas {
	return gas.Gas(f)
}
