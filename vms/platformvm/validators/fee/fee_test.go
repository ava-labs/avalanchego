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
		name                string
		target              gas.Gas
		current             gas.Gas
		excess              gas.Gas
		duration            uint64
		cost                uint64
		excessAfterDuration gas.Gas
	}{
		{
			name:                "excess=0, current<target, minute",
			target:              10_000,
			current:             10,
			excess:              0,
			duration:            minute,
			cost:                122_880,
			excessAfterDuration: 0,
		},
		{
			name:                "excess=0, current=target, minute",
			target:              10_000,
			current:             10_000,
			excess:              0,
			duration:            minute,
			cost:                122_880,
			excessAfterDuration: 0,
		},
		{
			name:                "excess=excessIncreasePerDoubling, current=target, minute",
			target:              10_000,
			current:             10_000,
			excess:              excessIncreasePerDoubling,
			duration:            minute,
			cost:                245_760,
			excessAfterDuration: excessIncreasePerDoubling,
		},
		{
			name:                "excess=K, current=target, minute",
			target:              10_000,
			current:             10_000,
			excess:              excessConversionConstant,
			duration:            minute,
			cost:                334_020,
			excessAfterDuration: excessConversionConstant,
		},
		{
			name:                "excess=0, current>target, minute",
			target:              10_000,
			current:             15_000,
			excess:              0,
			duration:            minute,
			cost:                122_880,
			excessAfterDuration: 5_000 * minute,
		},
		{
			name:                "excess hits 0 during, current<target, day",
			target:              10_000,
			current:             9_000,
			excess:              6 * hour * 1_000,
			duration:            day,
			cost:                177_321_939,
			excessAfterDuration: 0,
		},
		{
			name:                "excess=K, current=target, day",
			target:              10_000,
			current:             10_000,
			excess:              excessConversionConstant,
			duration:            day,
			cost:                480_988_800,
			excessAfterDuration: excessConversionConstant,
		},
		{
			name:                "excess=0, current>target, day",
			target:              10_000,
			current:             15_000,
			excess:              0,
			duration:            day,
			cost:                211_438_809,
			excessAfterDuration: 5_000 * day,
		},
		{
			name:                "excess=0, current=target, week",
			target:              10_000,
			current:             10_000,
			excess:              0,
			duration:            week,
			cost:                1_238_630_400,
			excessAfterDuration: 0,
		},
		{
			name:                "excess=0, current>target, week",
			target:              10_000,
			current:             15_000,
			excess:              0,
			duration:            week,
			cost:                5_265_492_669,
			excessAfterDuration: 5_000 * week,
		},
	}
)

func TestCalculateExcess(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			excess := CalculateExcess(
				test.target,
				test.current,
				test.excess,
				test.duration,
			)
			require.Equal(t, test.excessAfterDuration, excess)
		})
	}
}

func TestCalculateCost(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cost := CalculateCost(
				test.target,
				test.current,
				minPrice,
				excessConversionConstant,
				test.excess,
				test.duration,
			)
			require.Equal(t, test.cost, cost)
		})
	}
}

func TestCalculateDuration(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			duration := CalculateDuration(
				test.target,
				test.current,
				minPrice,
				excessConversionConstant,
				test.excess,
				year,
				test.cost,
			)
			require.Equal(t, test.duration, duration)
		})
	}
}

func BenchmarkCalculateCost(b *testing.B) {
	benchmarks := []struct {
		name          string
		calculateCost func(
			target gas.Gas,
			current gas.Gas,
			minPrice gas.Price,
			excessConversionConstant gas.Gas,
			excess gas.Gas,
			duration uint64,
		) uint64
	}{
		{
			name:          "unoptimized",
			calculateCost: unoptimizedCalculateCost,
		},
		{
			name:          "optimized",
			calculateCost: CalculateCost,
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			for _, benchmark := range benchmarks {
				b.Run(benchmark.name, func(b *testing.B) {
					for i := 0; i < b.N; i++ {
						benchmark.calculateCost(
							test.target,
							test.current,
							minPrice,
							excessConversionConstant,
							test.excess,
							test.duration,
						)
					}
				})
			}
		})
	}
}

func BenchmarkCalculateDuration(b *testing.B) {
	benchmarks := []struct {
		name              string
		calculateDuration func(
			target gas.Gas,
			current gas.Gas,
			minPrice gas.Price,
			excessConversionConstant gas.Gas,
			excess gas.Gas,
			maxDuration uint64,
			targetCost uint64,
		) uint64
	}{
		{
			name:              "unoptimized",
			calculateDuration: unoptimizedCalculateDuration,
		},
		{
			name:              "optimized",
			calculateDuration: CalculateDuration,
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			for _, benchmark := range benchmarks {
				b.Run(benchmark.name, func(b *testing.B) {
					for i := 0; i < b.N; i++ {
						benchmark.calculateDuration(
							test.target,
							test.current,
							minPrice,
							excessConversionConstant,
							test.excess,
							year,
							test.cost,
						)
					}
				})
			}
		})
	}
}

func FuzzCalculateCost(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			target uint64,
			current uint64,
			minPrice uint64,
			excessConversionConstant uint64,
			excess uint64,
			duration uint64,
		) {
			excessConversionConstant = max(excessConversionConstant, 1)
			duration = min(duration, year)

			expected := unoptimizedCalculateCost(
				gas.Gas(target),
				gas.Gas(current),
				gas.Price(minPrice),
				gas.Gas(excessConversionConstant),
				gas.Gas(excess),
				duration,
			)
			actual := CalculateCost(
				gas.Gas(target),
				gas.Gas(current),
				gas.Price(minPrice),
				gas.Gas(excessConversionConstant),
				gas.Gas(excess),
				duration,
			)
			require.Equal(t, expected, actual)
		},
	)
}

func FuzzCalculateDuration(f *testing.F) {
	f.Fuzz(
		func(
			t *testing.T,
			target uint64,
			current uint64,
			minPrice uint64,
			excessConversionConstant uint64,
			excess uint64,
			maxDuration uint64,
			targetCost uint64,
		) {
			excessConversionConstant = max(excessConversionConstant, 1)
			maxDuration = min(maxDuration, year)

			expected := unoptimizedCalculateDuration(
				gas.Gas(target),
				gas.Gas(current),
				gas.Price(minPrice),
				gas.Gas(excessConversionConstant),
				gas.Gas(excess),
				maxDuration,
				targetCost,
			)
			actual := CalculateDuration(
				gas.Gas(target),
				gas.Gas(current),
				gas.Price(minPrice),
				gas.Gas(excessConversionConstant),
				gas.Gas(excess),
				maxDuration,
				targetCost,
			)
			require.Equal(t, expected, actual)
		},
	)
}

// unoptimizedCalculateCost is a naive implementation of CalculateCost that is
// used for fuzzing.
func unoptimizedCalculateCost(
	target gas.Gas,
	current gas.Gas,
	minPrice gas.Price,
	excessConversionConstant gas.Gas,
	excess gas.Gas,
	duration uint64,
) uint64 {
	var (
		cost uint64
		err  error
	)
	for i := uint64(0); i < duration; i++ {
		excess = CalculateExcess(target, current, excess, 1)
		price := gas.CalculatePrice(minPrice, excess, excessConversionConstant)
		cost, err = safemath.Add(cost, uint64(price))
		if err != nil {
			return math.MaxUint64
		}
	}
	return cost
}

// unoptimizedCalculateDuration is a naive implementation of CalculateDuration
// that is used for fuzzing.
func unoptimizedCalculateDuration(
	target gas.Gas,
	current gas.Gas,
	minPrice gas.Price,
	excessConversionConstant gas.Gas,
	excess gas.Gas,
	maxDuration uint64,
	targetCost uint64,
) uint64 {
	var (
		cost     uint64
		duration uint64
		err      error
	)
	for cost < targetCost && duration < maxDuration {
		duration++
		excess = CalculateExcess(target, current, excess, 1)
		price := gas.CalculatePrice(minPrice, excess, excessConversionConstant)
		cost, err = safemath.Add(cost, uint64(price))
		if err != nil {
			return duration
		}
	}
	return duration
}

// floatToGas converts f to gas.Gas by truncation. `gas.Gas(f)` is preferred and
// floatToGas should only be used if its argument is a `const`.
func floatToGas(f float64) gas.Gas {
	return gas.Gas(f)
}
