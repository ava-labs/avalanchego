// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"math"
	"math/big"
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
	for _, s := range []struct {
		T, x, M, KonT         uint64
		newT, newM, newKonT   uint64
		initStatic, newStatic bool
	}{
		// Basic scaling change: K doubles, price should be maintained
		{
			T: 1e6, M: 1, KonT: 1, // i.e. K == 1e6
			x:    2e6,          // Initial price is M⋅exp(x/K) = exp(2/1) ~= 7
			newT: 1e6, newM: 1, // both unchanged
			newKonT: 2, // i.e. K == 2e6; without proper scaling, price becomes exp(2/2) ~= 2
		},
		// K at MaxUint64 boundary
		{
			T: 1e6, M: 1, KonT: math.MaxUint64,
			x:    2e6,
			newT: 1e6, newM: 1,
			newKonT: math.MaxUint64,
		},
		// MinPrice increase above current price: price should bump to new M
		{
			T: 1e6, M: 1, KonT: 87,
			x:       2e6, // price = 1 * e^(2/87) ~= 1.023
			newT:    1e6,
			newM:    100, // new M > current price, should bump
			newKonT: 87,
		},
		// MinPrice decrease: price should be maintained
		{
			T: 1e6, M: 100, KonT: 87,
			x:       1e6, // price > 100
			newT:    1e6,
			newM:    50, // M decreases
			newKonT: 87,
		},
		// MinPrice increase below current price: price should be maintained
		{
			T: 1e6, M: 1, KonT: 87,
			x:       100e6, // high excess = high price >> 10
			newT:    1e6,
			newM:    10, // new M < current price
			newKonT: 87,
		},
		// Large excess value
		{
			T: 1e6, M: 1, KonT: 87,
			x:       1e9, // very high excess
			newT:    1e6,
			newM:    1,
			newKonT: 100,
		},
		// High MinPrice with scaling change
		{
			T: 1e6, M: 1e9, KonT: 87,
			x:       5e6,
			newT:    1e6,
			newM:    1e9,
			newKonT: 50,
		},
		// Zero excess with config changes
		{
			T: 1e6, M: 1, KonT: 87,
			x:       0,
			newT:    1e6,
			newM:    1,
			newKonT: 50,
		},
		// Around MaxUint64 scaling with MinPrice decrease
		{
			T: 1e6, M: 26, KonT: math.MaxInt64 - 100,
			x:       1e6,
			newT:    1e6,
			newM:    1,
			newKonT: math.MaxInt64 - 10,
		},
		// MaxUint64 scaling with high excess min price at 11 gwei
		{
			T: 1e6, M: 1e12, KonT: 87,
			x:       1e9,
			newT:    1e6,
			newM:    1e12,
			newKonT: math.MaxUint64,
		},
		// Dynamic to static pricing: price should snap to newMinPrice
		{
			T: 1e6, M: 1, KonT: 87,
			x:         1e9, // high excess = high price
			newT:      1e6,
			newM:      1,
			newKonT:   87,
			newStatic: true,
		},
		// Static to dynamic pricing: price continuity from initMinPrice
		{
			T: 1e6, M: 100, KonT: 87,
			x:          5e6,
			initStatic: true,
			newT:       1e6,
			newM:       50, // M decreases, should maintain initMinPrice via excess
			newKonT:    87,
		},
		// Static to static with MinPrice change: price should be newMinPrice
		{
			T: 1e6, M: 100, KonT: 87,
			x:          5e6,
			initStatic: true,
			newT:       1e6,
			newM:       200,
			newKonT:    87,
			newStatic:  true,
		},
		{
			KonT: 0, // invalid
			T:    1, M: 1,
		},
		{
			M: 0, // invalid
			T: 1, KonT: 1,
		},
		{
			newM: 0, // invalid
			T:    1, M: 1, KonT: 1,
			newKonT: 1,
		},
		{
			newKonT: 0, // invalid
			T:       1, M: 1, KonT: 1,
			newM: 1,
		},
	} {
		f.Add(s.T, s.x, s.M, s.KonT, s.initStatic, s.newT, s.newM, s.newKonT, s.newStatic)
	}

	f.Fuzz(func(
		t *testing.T,
		initTarget, initExcess, initMinPrice, initScaling uint64, initStaticPricing bool,
		// There is no `newExcess` because it's computed (and tested).
		newTarget, newMinPrice, newScaling uint64, newStaticPricing bool,
	) {
		if initScaling > 1e6 || newScaling > 1e6 {
			// The scaling factor controls the rate of gas-price doubling, where
			// 87 is approximately half an hour, and price is inversely
			// proportional to the value.
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

		initExp := tm.exponent()
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

		switch minP := gas.Price(newMinPrice); {
		case minP > initPrice:
			require.Equal(t, minP, tm.Price(), "minimum price > initial price -> price == min")

		case newStaticPricing:
			require.Equal(t, minP, tm.Price(), "static pricing -> price == min")

		case tm.config.equalHookConfig(t, initConfig):
			// Target-only changes keep the x/K exponent as close to equal as
			// possible given the resolution of the denominator.
			newExp := tm.exponent()

			diff := new(big.Rat).Sub(newExp, initExp)
			diff.Abs(diff)

			tolerance := new(big.Rat).SetFrac(
				big.NewInt(1),
				newExp.Denom(),
			)
			if diff.Cmp(tolerance) >= 0 {
				t.Errorf("Exponent of price equation changed from %v to %v; diff = %v >= %v", initExp, newExp, diff, tolerance)
			}

		default:
			// Due to integer arithmetic in binary search, exact price
			// continuity isn't always achievable. [Time.findExcessForPrice]
			// finds the lowest excess that results in >= the targeted price.
			assert.GreaterOrEqual(t, tm.Price(), initPrice, "binary search on excess results in price >= initial")
			if tm.excess > 0 {
				tm.excess--
				assert.Less(t, tm.Price(), initPrice, "binary search on excess results in lowest price >= initial")
			}
		}
	})
}

// equalHookConfig is equivalent to [config.equal] but accepts a
// [hook.GasPriceConfig] that is first converted and validated.
func (c *config) equalHookConfig(tb testing.TB, hookCfg hook.GasPriceConfig) bool {
	tb.Helper()
	other, err := newConfig(hookCfg)
	require.NoError(tb, err)
	return c.equals(other)
}

// exponent returns x/K, the exponent of the gas price. For fixed M, equal
// exponents result in equal prices. However if scaling results in a new
// exponent with insufficient resolution (too low a denominator) to be identical
// then the price could be different.
func (tm *Time) exponent() *big.Rat {
	return new(big.Rat).SetFrac(
		new(big.Int).SetUint64(uint64(tm.excess)),
		new(big.Int).SetUint64(uint64(tm.excessScalingFactor())),
	)
}

// TestOscillatingMinPrice verifies that oscillating MinPrice between two values
// does not impact gas price growth. When blocks consistently consume
// above-target gas, the price should increase over time regardless of MinPrice
// changes.
func TestOscillatingMinPrice(t *testing.T) {
	const (
		target       gas.Gas   = 1_000_000
		scaling      gas.Gas   = 87
		gasPerBlock  gas.Gas   = target // value doesn't actually matter, we never idle in the test
		numBlocks              = 1000
		highMinPrice gas.Price = 2
		lowMinPrice  gas.Price = 1
	)

	initialConfig := hook.GasPriceConfig{
		TargetToExcessScaling: scaling,
		MinPrice:              highMinPrice,
	}
	control := mustNew(t, time.Unix(0, 0), target, 0, initialConfig)
	modified := mustNew(t, time.Unix(0, 0), target, 0, initialConfig)

	require.Equal(t, highMinPrice, control.Price())
	require.Equal(t, highMinPrice, modified.Price())

	for i := range numBlocks {
		require.NoError(t, control.AfterBlock(
			gasPerBlock,
			hookstest.NewStub(target, hookstest.WithGasPriceConfig(initialConfig)),
			nil,
		))

		oscillatingConfig := initialConfig
		if i%2 == 0 {
			oscillatingConfig.MinPrice = lowMinPrice
		}
		require.NoError(t, modified.AfterBlock(
			gasPerBlock,
			hookstest.NewStub(target, hookstest.WithGasPriceConfig(oscillatingConfig)),
			nil,
		))
	}

	// Sanity check that price normally increases.
	assert.Greater(t, control.Price(), highMinPrice, "control price must increase with sustained above-target usage")
	assert.Equal(t, control.Price(), modified.Price(), "alternating MinPrice must not impact price growth")
}
