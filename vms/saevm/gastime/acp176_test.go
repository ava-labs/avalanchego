// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"math"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/intmath"
)

// TestInvalidConfigRejected verifies that zero values for TargetToExcessScaling
// and MinPrice are rejected by AfterBlock. This is a defensive test to ensure
// that the gas price calculation will not be broken by `AfterBlock`.
func TestInvalidConfigRejected(t *testing.T) {
	const target = gas.Gas(1_000_000)

	tests := []struct {
		name     string
		config   hook.GasPriceConfig
		expected error
	}{
		{
			"zero_scaling",
			hook.GasPriceConfig{TargetToExcessScaling: 0, MinPrice: DefaultGasPriceConfig().MinPrice},
			errInvalidGasPriceConfig,
		},
		{
			"zero_min_price",
			hook.GasPriceConfig{TargetToExcessScaling: DefaultGasPriceConfig().TargetToExcessScaling, MinPrice: 0},
			errInvalidGasPriceConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := mustNew(t, time.Unix(42, 0), target, 0, DefaultGasPriceConfig())

			initialScaling := tm.config.targetToExcessScaling
			initialMinPrice := tm.config.minPrice
			hooks := hookstest.NewStub(target, hookstest.WithGasPriceConfig(tt.config))
			err := tm.AfterBlock(0, hooks, &types.Header{Time: 42})
			require.ErrorIs(t, err, tt.expected, "AfterBlock()")

			// Config unchanged after rejected update
			assert.Equal(t, initialScaling, tm.config.targetToExcessScaling, "targetToExcessScaling changed")
			assert.Equal(t, initialMinPrice, tm.config.minPrice, "minPrice changed")
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
		newTime   uint64 = initialTime + 1
		newTarget        = initialTarget + 100_000
	)
	hook := hookstest.NewStub(newTarget)
	header := &types.Header{
		Time: newTime,
	}

	initialPrice := tm.Price()
	tm.BeforeBlock(hook, header)
	assert.Equal(t, newTime, tm.Unix(), "Unix time advanced by BeforeBlock()")
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
	require.NoError(t, tm.AfterBlock(used, hook, header), "AfterBlock()")
	assert.Equal(t, expectedEndTime, tm.Unix(), "Unix time advanced by AfterBlock() due to gas consumption")
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

			header := &types.Header{
				Time: block.time,
			}
			hook := hookstest.NewStub(block.target, hookstest.WithNow(func() time.Time {
				return time.Unix(
					int64(block.time), //#nosec G115 -- Won't overflow for a few millennia
					int64(block.nanos),
				)
			}))

			worstcase.BeforeBlock(hook, header)
			actual.BeforeBlock(hook, header)

			// The crux of this test lies in the maintaining of this inequality
			// through the use of `limit` instead of `used` in `AfterBlock()`
			require.LessOrEqualf(t, actual.Price(), worstcase.Price(), "actual <= worst-case %T.Price()", actual)
			require.NoError(t, worstcase.AfterBlock(block.limit, hook, header), "worstcase.AfterBlock()")
			require.NoError(t, actual.AfterBlock(block.used, hook, header), "actual.AfterBlock()")
		}
	})
}

func FuzzPriceInvarianceAfterBlock(f *testing.F) {
	type state struct {
		target        uint64
		excess        uint64
		minPrice      uint64
		scaling       uint64
		staticPricing bool
	}
	for _, s := range []struct {
		init   state
		change state
	}{
		// Basic scaling change: K doubles, price should be maintained
		{
			init: state{
				target:   1e6,
				excess:   2e6, // Initial price is M⋅exp(x/K) = exp(2/1) ~= 7
				minPrice: 1,
				scaling:  1, // K == T
			},
			change: state{
				scaling: 2, // doubled; without proper scaling, price becomes exp(2/2) ~= 2
			},
		},
		// MinPrice increase above current price: price should bump to new M
		{
			init: state{
				target:   1e6,
				excess:   2e6, // price = 1 * e^(2/87) ~= 1.023
				minPrice: 1,
				scaling:  87,
			},
			change: state{
				minPrice: 100, // 100x initial
			},
		},
		// MinPrice decrease: price should be maintained
		{
			init: state{
				target:   1e6,
				excess:   1e6, // price = 1 * e^(1/87) ~= 1.012
				minPrice: 100,
				scaling:  87,
			},
			change: state{
				minPrice: 50, // halved
			},
		},
		// MinPrice increase below current price: price should be maintained
		{
			init: state{
				target:   1e6,
				excess:   100e6, // high excess = high price >> 10
				minPrice: 1,
				scaling:  87,
			},
			change: state{
				minPrice: 10, // new M < current price
			},
		},
		// Large excess value
		{
			init: state{
				target:   1e6,
				excess:   1e9, // very high excess
				minPrice: 1,
				scaling:  87,
			},
			change: state{
				scaling: 100,
			},
		},
		// High MinPrice with scaling change
		{
			init: state{
				target:   1e6,
				excess:   5e6,
				minPrice: 1e9,
				scaling:  87,
			},
			change: state{
				scaling: 50,
			},
		},
		// Zero excess with config changes
		{
			init: state{
				target:   1e6,
				excess:   0,
				minPrice: 1,
				scaling:  87,
			},
			change: state{
				scaling: 50,
			},
		},
		// Dynamic to static pricing: price should snap to newMinPrice
		{
			init: state{
				target:   1e6,
				excess:   1e9, // high excess = high price
				minPrice: 1,
				scaling:  87,
			},
			change: state{
				staticPricing: true,
			},
		},
		// Static to dynamic pricing: price continuity from initMinPrice
		{
			init: state{
				target:        1e6,
				excess:        5e6,
				minPrice:      100,
				scaling:       87,
				staticPricing: true,
			},
			change: state{
				minPrice:      50, // M decreases, should maintain initMinPrice via excess
				staticPricing: true,
			},
		},
		// Static to static with MinPrice change: price should be newMinPrice
		{
			init: state{
				target:        1e6,
				excess:        5e6,
				minPrice:      100,
				scaling:       87,
				staticPricing: true,
			},
			change: state{
				minPrice: 200,
			},
		},
		{
			init: state{
				target:   1,
				minPrice: 1,
				scaling:  0, // invalid
			},
		},
		{
			init: state{
				target:   1,
				minPrice: 0, // invalid
				scaling:  1,
			},
		},
	} {
		newState := s.change
		if s.change.target == 0 {
			newState.target = s.init.target
		}
		if s.change.minPrice == 0 {
			newState.minPrice = s.init.minPrice
		}
		if s.change.scaling == 0 {
			newState.scaling = s.init.scaling
		}
		newState.staticPricing = s.init.staticPricing
		if s.change.staticPricing {
			newState.staticPricing = !s.init.staticPricing
		}
		f.Add(
			s.init.target, s.init.excess, s.init.minPrice, s.init.scaling, s.init.staticPricing,
			newState.target, newState.minPrice, newState.scaling, newState.staticPricing,
		)
	}

	f.Fuzz(func(
		t *testing.T,
		initTarget, initExcess, initMinPrice, initScaling uint64, initStaticPricing bool,
		// There is no `newExcess` because it's computed (and tested).
		newTarget, newMinPrice, newScaling uint64, newStaticPricing bool,
	) {
		if initScaling > 1e6 || newScaling > 1e6 {
			// The scaling factor controls the rate of gas-price doubling, where
			// 87 is approximately one minute.
			t.Skip("Excessive scaling")
		}

		// Avoid having to deal with weird edge cases for extremely low targets
		// due to insufficient resolution in the rational exponent.
		switch minRate := gas.Gas(params.TxGas); {
		case SafeRateOfTarget(gas.Gas(initTarget)) < minRate:
			t.Skip("Initial target too low")
		case SafeRateOfTarget(gas.Gas(newTarget)) < minRate:
			t.Skip("New target too low")
		}

		initConfig := hook.GasPriceConfig{
			TargetToExcessScaling: gas.Gas(initScaling),
			MinPrice:              gas.Price(initMinPrice),
			StaticPricing:         initStaticPricing,
		}
		tm, err := New(
			time.Unix(0, 0),
			gas.Gas(initTarget),
			gas.Gas(initExcess),
			initConfig,
		)

		{
			var wantErrIs error
			if initScaling == 0 || initMinPrice == 0 {
				wantErrIs = errInvalidGasPriceConfig
			}
			require.ErrorIsf(t, err, wantErrIs, "New(... %+v)", initConfig)
			if wantErrIs != nil {
				return
			}
		}

		initExcess = uint64(tm.excess)
		initPrice := tm.Price()

		{
			hooks := hookstest.NewStub(
				gas.Gas(newTarget),
				hookstest.WithGasPriceConfig(hook.GasPriceConfig{
					MinPrice:              gas.Price(newMinPrice),
					TargetToExcessScaling: gas.Gas(newScaling),
					StaticPricing:         newStaticPricing,
				}))

			var wantErrIs error
			if newScaling == 0 || newMinPrice == 0 {
				wantErrIs = errInvalidGasPriceConfig
			}

			// Consuming gas increases the excess, which changes the price.
			// We're only interested in invariance under changes in config.
			const gasUsed = 0
			err := tm.AfterBlock(gasUsed, hooks, nil)
			require.ErrorIsf(t, err, wantErrIs, "AfterBlock([%+v])", hooks)
			if wantErrIs != nil {
				return
			}
		}

		pr := message.NewPrinter(language.English)
		log := func(desc string, from, to uint64) {
			if from == to {
				t.Log(pr.Sprintf("%s (unchanged): %d", desc, from))
			} else {
				t.Log(pr.Sprintf("%s: %d -> %d", desc, from, to))
			}
		}
		log("Target", initTarget, newTarget)
		log("MinPrice", initMinPrice, newMinPrice)
		log("TargetToExcessScaling", initScaling, newScaling)
		t.Logf("StaticPricing: %t -> %t", initStaticPricing, newStaticPricing)
		log("Excess", initExcess, uint64(tm.excess))
		log("Price (value under test)", uint64(initPrice), uint64(tm.Price()))

		minP := gas.Price(newMinPrice)
		assert.GreaterOrEqual(t, tm.Price(), minP, "price >= minimum price")
		if newStaticPricing {
			assert.Equal(t, minP, tm.Price(), "static pricing -> price == min")
			return
		}

		// Integer approximation may cause the price to be slightly lowered, but
		// it should be marginal.
		{
			allowedPrice, _, err := intmath.MulDiv(initPrice, 999, 1000) // 99.9% accurate
			require.NoError(t, err, "intmath.MulDiv unexpected error")
			assert.GreaterOrEqual(t, tm.Price(), allowedPrice, "price >= allowed price")
		}

		if tm.config.minPrice > initPrice {
			assert.Equal(t, tm.config.minPrice, tm.Price(), "price == minimum price")
			// Due to integer arithmetic in binary search, exact price
			// continuity isn't always achievable. [Time.findExcessForPrice]
			// finds the lowest excess that results in >= the targeted price.
			if calculatePrice(tm.excess, tm.excessScalingFactor()) < tm.config.minPrice {
				tm.excess++
				assert.Greater(t, tm.Price(), tm.config.minPrice, "binary search on excess results in lowest price >= initial")
			}
		}
	})
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

	require.Equal(t, highMinPrice, control.Price())
	require.Equal(t, highMinPrice, modified.Price())

	for i := range numBlocks {
		require.NoError(t, control.AfterBlock(
			gasPerBlock,
			hookstest.NewStub(target, hookstest.WithGasPriceConfig(highPriceConfig)),
			nil,
		))

		oscillatingConfig := highPriceConfig
		if i%2 == 0 {
			oscillatingConfig = lowPriceConfig
		}
		require.NoError(t, modified.AfterBlock(
			gasPerBlock,
			hookstest.NewStub(target, hookstest.WithGasPriceConfig(oscillatingConfig)),
			nil,
		))
	}

	// Sanity check that price normally increases.
	assert.Greater(t, control.Price(), highMinPrice, "control price must increase with sustained above-target usage")
	assert.Equal(t, control.Price(), modified.Price(), "oscillating MinPrice must not impact price growth")
}

func TestStaticPriceRemoval(t *testing.T) {
	const target gas.Gas = 1_000_000
	staticConfig := DefaultGasPriceConfig()
	staticConfig.StaticPricing = true
	g := mustNew(t, time.Unix(0, 0), target, 0, staticConfig)

	const largeGas gas.Gas = 1000 * target // must be sufficiently large
	g.Tick(largeGas)

	var (
		want          = g.Price() // static price
		dynamicConfig = DefaultGasPriceConfig()
	)
	const noGas gas.Gas = 0
	require.NoError(t, g.AfterBlock(
		noGas,
		hookstest.NewStub(target, hookstest.WithGasPriceConfig(dynamicConfig)),
		nil,
	))
	assert.Equal(t, want, g.Price(), "price invariant during static price removal")
}
