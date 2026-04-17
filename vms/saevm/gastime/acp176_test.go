// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// TestInvalidConfigRejected verifies that zero values for TargetToExcessScaling
// and MinPrice are rejected by AfterBlock. This is a defensive test to ensure
// that the gas price calculation will not be broken by `AfterBlock`.
func TestInvalidConfigRejected(t *testing.T) {
	const target = gas.Gas(1_000_000)

	tests := []struct {
		name     string
		config   GasPriceConfig
		expected error
	}{
		{
			"zero_scaling",
			GasPriceConfig{TargetToExcessScaling: 0, MinPrice: DefaultGasPriceConfig().MinPrice},
			errTargetToExcessScalingZero,
		},
		{
			"zero_min_price",
			GasPriceConfig{TargetToExcessScaling: DefaultGasPriceConfig().TargetToExcessScaling, MinPrice: 0},
			errMinPriceZero,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := mustNew(t, time.Unix(42, 0), target, 0, DefaultGasPriceConfig())

			initialScaling := tm.config.TargetToExcessScaling
			initialMinPrice := tm.config.MinPrice
			err := tm.AfterBlock(0, target, tt.config)
			require.ErrorIs(t, err, tt.expected, "AfterBlock()")

			// Config unchanged after rejected update
			assert.Equal(t, initialScaling, tm.config.TargetToExcessScaling, "targetToExcessScaling changed")
			assert.Equal(t, initialMinPrice, tm.config.MinPrice, "minPrice changed")
		})
	}
}

// TestTargetUpdateTiming verifies that the gas target is modified in AfterBlock
// rather than BeforeBlock.
func TestTargetUpdateTiming(t *testing.T) {
	const (
		initialTime           = 42
		initialTarget gas.Gas = 1_600_000
		initialExcess         = 1_234_567_890
	)
	tm := mustNew(t, time.Unix(initialTime, 0), initialTarget, initialExcess, DefaultGasPriceConfig())
	initialRate := tm.Rate()

	const (
		newTime   = initialTime + 1
		newTarget = initialTarget + 100_000
	)

	initialPrice := tm.Price()
	tm.BeforeBlock(time.Unix(newTime, 0))
	assert.Equal(t, uint64(newTime), tm.Unix(), "Unix time advanced by BeforeBlock()")
	assert.Equal(t, initialTarget, tm.Target(), "Target not changed by BeforeBlock()")
	// While the price technically could remain the same, being more strict
	// ensures the test is meaningful.
	enforcedPrice := tm.Price()
	assert.Less(t, enforcedPrice, initialPrice, "Price should not increase in BeforeBlock()")
	if t.Failed() {
		t.FailNow()
	}

	const (
		secondsOfGasUsed = 3
		expectedEndTime  = newTime + secondsOfGasUsed
	)
	used := initialRate * secondsOfGasUsed
	require.NoError(t, tm.AfterBlock(used, newTarget, DefaultGasPriceConfig()), "AfterBlock()")
	assert.Equal(t, uint64(expectedEndTime), tm.Unix(), "Unix time advanced by AfterBlock() due to gas consumption")
	assert.Equal(t, newTarget, tm.Target(), "Target updated by AfterBlock()")
	// While the price technically could remain the same, being more strict
	// ensures the test is meaningful.
	assert.Greater(t, tm.Price(), enforcedPrice, "Price should not decrease in AfterBlock()")
}

func FuzzWorstCasePrice(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		initTimestamp, initTarget, initExcess,
		time0, nanos0, used0, limit0, target0,
		time1, nanos1, used1, limit1, target1,
		time2, nanos2, used2, limit2, target2,
		time3, nanos3, used3, limit3, target3 uint64,
	) {
		initTarget = max(initTarget, 1)
		gasCfg := DefaultGasPriceConfig()

		initUnix := int64(min(initTimestamp, math.MaxInt64)) //#nosec G115 -- Clamped to MaxInt64
		worstcase := mustNew(t, time.Unix(initUnix, 0), gas.Gas(initTarget), gas.Gas(initExcess), DefaultGasPriceConfig())
		actual := mustNew(t, time.Unix(initUnix, 0), gas.Gas(initTarget), gas.Gas(initExcess), DefaultGasPriceConfig())

		blocks := []struct {
			time   uint64
			nanos  time.Duration
			used   gas.Gas
			limit  gas.Gas
			target gas.Gas
		}{
			{
				time:   time0,
				nanos:  time.Duration(nanos0 % 1e9), //#nosec G115
				used:   gas.Gas(used0),
				limit:  gas.Gas(limit0),
				target: gas.Gas(target0),
			},
			{
				time:   time1,
				nanos:  time.Duration(nanos1 % 1e9), //#nosec G115
				used:   gas.Gas(used1),
				limit:  gas.Gas(limit1),
				target: gas.Gas(target1),
			},
			{
				time:   time2,
				nanos:  time.Duration(nanos2 % 1e9), //#nosec G115
				used:   gas.Gas(used2),
				limit:  gas.Gas(limit2),
				target: gas.Gas(target2),
			},
			{
				time:   time3,
				nanos:  time.Duration(nanos3 % 1e9), //#nosec G115
				used:   gas.Gas(used3),
				limit:  gas.Gas(limit3),
				target: gas.Gas(target3),
			},
		}
		for _, block := range blocks {
			block.limit = max(block.used, block.limit)
			block.target = clampTarget(max(block.target, 1))
			blockSeconds := int64(min(block.time, math.MaxInt64)) //#nosec G115 -- Clamped to MaxInt64

			tm := time.Unix(blockSeconds, int64(block.nanos))
			worstcase.BeforeBlock(tm)
			actual.BeforeBlock(tm)

			// The crux of this test lies in the maintaining of this inequality
			// through the use of `limit` instead of `used` in `AfterBlock()`
			require.LessOrEqualf(t, actual.Price(), worstcase.Price(), "actual <= worst-case %T.Price()", actual)
			require.NoError(t, worstcase.AfterBlock(block.limit, block.target, gasCfg), "worstcase.AfterBlock()")
			require.NoError(t, actual.AfterBlock(block.used, block.target, gasCfg), "actual.AfterBlock()")
		}
	})
}

func TestAfterBlock(t *testing.T) {
	type state struct {
		target gas.Gas
		excess gas.Gas
		config GasPriceConfig
		price  gas.Price
	}
	tests := []struct {
		name    string
		init    state
		gasUsed gas.Gas
		new     state
		wantErr error
	}{
		// Normal changes:
		{
			name: "target_doubles",
			init: state{
				target: 1_000_000,
				excess: 2_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 7,
			},
			new: state{
				target: 2_000_000,
				excess: 4_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 7,
			},
		},
		{
			name: "scaling_doubles",
			init: state{
				target: 1_000_000,
				excess: 2_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 7,
			},
			new: state{
				target: 1_000_000,
				excess: 4_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 2,
					MinPrice:              1,
				},
				price: 7,
			},
		},
		{
			name: "target_and_scaling_doubles",
			init: state{
				target: 1_000_000,
				excess: 2_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 7,
			},
			new: state{
				target: 2_000_000,
				excess: 8_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 2,
					MinPrice:              1,
				},
				price: 7,
			},
		},
		{
			name: "target_and_scaling_doubles_zero",
			init: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 1,
			},
			new: state{
				target: 2_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 2,
					MinPrice:              1,
				},
				price: 1,
			},
		},
		{
			name: "gas_used_no_config_change",
			init: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 1,
			},
			gasUsed: 1_000_000,
			new: state{
				target: 1_000_000,
				excess: 500_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 1,
			},
		},
		{
			name: "gas_used_before_scaling",
			init: state{
				target: 1_000_000,
				excess: 2_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 7,
			},
			gasUsed: 1_000_000,
			new: state{
				target: 2_000_000,
				excess: 5_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 12,
			},
		},
		{
			name: "min_price_increase_above_current",
			init: state{
				target: 1_000_000,
				excess: 2_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 1,
			},
			new: state{
				target: 1_000_000,
				excess: 400_649_807,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              100,
				},
				price: 100,
			},
		},
		{
			name: "min_price_unrepresentable",
			init: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 1,
			},
			new: state{
				target: 1_000_000,
				excess: 1_802_924_127,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1_000_000_000,
				},
				price: 1_000_000_000,
			},
		},
		{
			name: "min_price_previously_unrepresentable",
			init: state{
				target: 1_000_000,
				excess: 1_802_924_127,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1_000_000_000,
				},
				price: 1_000_000_000,
			},
			new: state{
				target: 1_000_000,
				excess: 1_802_924_127,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 999_999_990,
			},
		},
		{
			name: "min_price_increase_below_current",
			init: state{
				target: 1_000_000,
				excess: 400_649_807,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 100,
			},
			new: state{
				target: 1_000_000,
				excess: 400_649_807,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              50,
				},
				price: 100,
			},
		},
		{
			name: "min_price_decrease",
			init: state{
				target: 1_000_000,
				excess: 400_649_807,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              100,
				},
				price: 100,
			},
			new: state{
				target: 1_000_000,
				excess: 400_649_807,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 100,
			},
		},

		// Static pricing:
		{
			name: "static_pricing_with_gas_used",
			init: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
					StaticPricing:         true,
				},
				price: 1,
			},
			gasUsed: 1_000_000,
			new: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
					StaticPricing:         true,
				},
				price: 1,
			},
		},
		{
			name: "static_pricing_overrides_excess",
			init: state{
				target: 1_000_000,
				excess: 1_000_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 98_150,
			},
			new: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
					StaticPricing:         true,
				},
				price: 1,
			},
		},
		{
			name: "static_pricing_removal_maintains_excess",
			init: state{
				target: 1_000_000,
				excess: 400_649_807,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              100,
					StaticPricing:         true,
				},
				price: 100,
			},
			new: state{
				target: 1_000_000,
				excess: 400_649_807,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              50,
				},
				price: 100,
			},
		},
		{
			name: "static_pricing_price_change",
			init: state{
				target: 1_000_000,
				excess: 400_649_807,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              100,
					StaticPricing:         true,
				},
				price: 100,
			},
			new: state{
				target: 1_000_000,
				excess: 460_953_612,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              200,
					StaticPricing:         true,
				},
				price: 200,
			},
		},

		// Extreme changes:
		{
			name: "scaling_to_max",
			init: state{
				target: 1_000_000,
				excess: 2_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 7,
			},
			new: state{
				target: 1_000_000,
				excess: math.MaxUint64,
				config: GasPriceConfig{
					TargetToExcessScaling: math.MaxUint64,
					MinPrice:              1,
				},
				price: 2,
			},
		},
		{
			name: "large_excess_scaling_change",
			init: state{
				target: 1_000_000,
				excess: 10_000_000_000_000_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: math.MaxUint64,
			},
			new: state{
				target: 1_000_000,
				excess: 11_494_252_873_563_218_391,
				config: GasPriceConfig{
					TargetToExcessScaling: 100,
					MinPrice:              1,
				},
				price: math.MaxUint64,
			},
		},
		{
			name: "high_price_scaling_causes_price_increase",
			init: state{
				target: 1_000_000,
				excess: 1_802_924_127,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 999_999_990,
			},
			new: state{
				target: 1_000_000,
				excess: 1_036_163_292,
				config: GasPriceConfig{
					TargetToExcessScaling: 50,
					MinPrice:              1,
				},
				price: 1_000_000_002,
			},
		},
		{
			name: "intermediate_scaling_overflow",
			init: state{
				target: 1_000_000,
				excess: math.MaxUint64 - 1,
				config: GasPriceConfig{
					TargetToExcessScaling: math.MaxUint64,
					MinPrice:              1,
				},
				price: 2,
			},
			new: state{
				target: 1_000_000,
				excess: 1_000_000,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 2,
			},
		},
		{
			name: "max_min_price",
			init: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              1,
				},
				price: 1,
			},
			new: state{
				target: 1_000_000,
				excess: 44_361_420,
				config: GasPriceConfig{
					TargetToExcessScaling: 1,
					MinPrice:              math.MaxUint64,
				},
				price: math.MaxUint64,
			},
		},

		// Invalid inputs:
		{
			name: "invalid_target_override",
			init: state{
				target: 0,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 1,
			},
			new: state{
				target: 1,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 1,
			},
		},
		{
			name: "invalid_zero_scaling",
			init: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 1,
			},
			new: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 0,
					MinPrice:              1,
				},
				price: 1,
			},
			wantErr: errTargetToExcessScalingZero,
		},
		{
			name: "invalid_zero_min_price",
			init: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              1,
				},
				price: 1,
			},
			new: state{
				target: 1_000_000,
				excess: 0,
				config: GasPriceConfig{
					TargetToExcessScaling: 87,
					MinPrice:              0,
				},
				price: 1,
			},
			wantErr: errMinPriceZero,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := mustNew(t, time.Unix(0, 0), tt.init.target, tt.init.excess, tt.init.config)
			assert.Equal(t, tt.init.excess, tm.Excess(), "init Excess")
			assert.Equal(t, tt.init.price, tm.Price(), "init Price")
			assert.Equal(t, tm.Target()*TargetToRate, tm.Rate(), "init Rate")

			err := tm.AfterBlock(tt.gasUsed, tt.new.target, tt.new.config)
			require.ErrorIsf(t, err, tt.wantErr, "%T.AfterBlock", tm)
			assert.Equal(t, tt.new.excess, tm.Excess(), "new Excess")
			assert.Equal(t, tt.new.price, tm.Price(), "new Price")
			assert.Equal(t, tm.Target()*TargetToRate, tm.Rate(), "new Rate")
		})
	}
}

// TestOscillatingMinPrice verifies that oscillating MinPrice between two values
// does not impact gas price growth. When blocks consistently consume
// above-target gas, the price should increase over time regardless of MinPrice
// changes.
func TestOscillatingMinPrice(t *testing.T) {
	const (
		target       gas.Gas   = 1_000_000
		gasPerBlock  gas.Gas   = target // must be sufficiently large
		numBlocks              = 1000
		highMinPrice gas.Price = 2
		lowMinPrice  gas.Price = 1
	)

	highPriceConfig := DefaultGasPriceConfig()
	highPriceConfig.MinPrice = highMinPrice

	lowPriceConfig := highPriceConfig
	lowPriceConfig.MinPrice = lowMinPrice

	control := mustNew(t, time.Unix(0, 0), target, 0, highPriceConfig)
	modified := mustNew(t, time.Unix(0, 0), target, 0, highPriceConfig)

	require.Equalf(t, highMinPrice, control.Price(), "%T.Price()", control)
	require.Equalf(t, highMinPrice, modified.Price(), "%T.Price()", modified)

	for i := range numBlocks {
		require.NoErrorf(t, control.AfterBlock(
			gasPerBlock,
			target,
			highPriceConfig,
		), "%T.AfterBlock(static)", control)

		oscillatingConfig := highPriceConfig
		if i%2 == 0 {
			oscillatingConfig = lowPriceConfig
		}
		require.NoErrorf(t, modified.AfterBlock(
			gasPerBlock,
			target,
			oscillatingConfig,
		), "%T.AfterBlock(oscillating)", modified)
	}

	// Sanity check that price normally increases.
	assert.Greater(t, control.Price(), highMinPrice, "control price must increase with sustained above-target usage")
	assert.Equal(t, control.Price(), modified.Price(), "oscillating MinPrice must not impact price growth")
}

func FuzzPriceExcess(f *testing.F) {
	seeds := []struct {
		p gas.Price
		k gas.Gas
	}{
		{1, 1},
		{2, 1},
		{2, 1_000_000_000},
		{1_000_000_000, 1},
		{2, math.MaxUint64},
		{math.MaxUint64, 1},
		{math.MaxUint64, math.MaxUint64},
	}
	for _, s := range seeds {
		f.Add(uint64(s.p), uint64(s.k))
	}
	f.Fuzz(func(t *testing.T, pInt, kInt uint64) {
		p, k := gas.Price(pInt), gas.Gas(kInt)
		if p == 0 {
			t.Skip("ln(0) is undefined")
		}
		if k == 0 {
			t.Skip("div by zero is undefined")
		}

		x := priceExcess(p, k)
		gotP := calculatePrice(x, k)
		assert.LessOrEqual(t, gotP, p, "gotPrice <= wantPrice")

		if gotP < p && x != math.MaxUint64 {
			require.Greater(t, calculatePrice(x+1, k), p, "calculatePrice(x+1) > wantPrice")
		}
		if gotP == p && x != 0 {
			require.Less(t, calculatePrice(x-1, k), p, "calculatePrice(x-1) < wantPrice")
		}
	})
}
