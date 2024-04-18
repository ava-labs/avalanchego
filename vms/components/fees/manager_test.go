// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpdateFeeRates(t *testing.T) {
	require := require.New(t)

	var (
		feesCfg = DynamicFeesConfig{
			MinFeeRate:                Dimensions{1, 1, 1, 1},
			UpdateCoefficient:         Dimensions{10_000, 20_000, 50_000, 100_000},
			BlockTargetComplexityRate: Dimensions{25, 25, 25, 25},
		}
		parentFeeRate    = Dimensions{1, 2, 10, 20}
		parentComplexity = Dimensions{100, 25, 20, 10}

		elapsedTime   = time.Second
		parentBlkTime = time.Now().Truncate(time.Second)
		childBlkTime  = parentBlkTime.Add(elapsedTime)
	)

	m := &Manager{
		feeRates: parentFeeRate,
	}

	require.NoError(m.UpdateFeeRates(
		feesCfg,
		parentComplexity,
		parentBlkTime.Unix(),
		childBlkTime.Unix(),
	))

	// Bandwidth complexity are above target, fee rate is pushed up
	require.Equal(uint64(16), m.feeRates[Bandwidth])

	// UTXORead complexity is at target, fee rate does not change
	require.Equal(parentFeeRate[UTXORead], m.feeRates[UTXORead])

	// UTXOWrite complexity is below target, fee rate is pushed down
	require.Equal(uint64(5), m.feeRates[UTXOWrite])

	// Compute complexoty is below target, fee rate is pushed down to the minimum
	require.Equal(feesCfg.MinFeeRate[Compute], m.feeRates[Compute])
}

func TestUpdateFeeRatesEdgeCases(t *testing.T) {
	require := require.New(t)

	var (
		feesCfg = DynamicFeesConfig{
			MinFeeRate:                Dimensions{1, 0, 0, 0},
			UpdateCoefficient:         Dimensions{10_000, 10_000, 10_000, 10_000},
			BlockTargetComplexityRate: Dimensions{math.MaxUint64, 1, 1, 1}, // a very skewed requirement for block complexity
		}
		parentFeeRate    = Dimensions{2, 1, 2, 2}
		parentComplexity = Dimensions{math.MaxUint64, 0, 1, math.MaxUint64}

		elapsedTime   = time.Second
		parentBlkTime = time.Now().Truncate(time.Second)
		childBlkTime  = parentBlkTime.Add(elapsedTime)
	)

	m := &Manager{feeRates: parentFeeRate}
	require.NoError(m.UpdateFeeRates(
		feesCfg,
		parentComplexity,
		parentBlkTime.Unix(),
		childBlkTime.Unix(),
	))

	// Bandwidth complexity is huge but at target, fee rate is unchanged
	require.Equal(parentFeeRate[Bandwidth], m.feeRates[Bandwidth])

	// UTXORead complexity is below target. Fee rate is reduced
	require.Equal(feesCfg.MinFeeRate[UTXORead], m.feeRates[UTXORead])

	// UTXOWrite complexity is at target, fee rate is unchanged
	require.Equal(parentFeeRate[UTXOWrite], m.feeRates[UTXOWrite])

	// Compute complexity is way above target. Fee rate spikes
	require.Equal(uint64(0x8000000000000000), m.feeRates[Compute])
}

func TestUpdateFeeRatesStability(t *testing.T) {
	// The advantage of using an exponential fee update scheme
	// (vs e.g. the EIP-1559 scheme we use in the C-chain) is that
	// it is more stable against dithering.
	// We prove here that if complexity oscillates around the target
	// fee rates are unchanged.

	require := require.New(t)

	var (
		feesCfg = DynamicFeesConfig{
			MinFeeRate:                Dimensions{0, 0, 0, 0},
			UpdateCoefficient:         Dimensions{20_000, 40_000, 50_000, 100_000},
			BlockTargetComplexityRate: Dimensions{25, 50, 100, 1000},
		}
		initialFeeRate   = Dimensions{10, 100, 1_000, 1_000_000_000}
		parentComplexity = Dimensions{24, 45, 70, 500}
		childComplexity  = Dimensions{26, 55, 130, 1500}

		elapsedTime      = time.Second
		parentBlkTime    = time.Now().Truncate(time.Second)
		childBlkTime     = parentBlkTime.Add(elapsedTime)
		granChildBlkTime = childBlkTime.Add(elapsedTime)
	)

	// step1: parent complexity is below target. Fee rates will decrease
	m1 := &Manager{feeRates: initialFeeRate}
	require.NoError(m1.UpdateFeeRates(
		feesCfg,
		parentComplexity,
		parentBlkTime.Unix(),
		childBlkTime.Unix(),
	))

	require.LessOrEqual(m1.feeRates[Bandwidth], initialFeeRate[Bandwidth])
	require.LessOrEqual(m1.feeRates[UTXORead], initialFeeRate[UTXORead])
	require.Less(m1.feeRates[UTXOWrite], initialFeeRate[UTXOWrite])
	require.Less(m1.feeRates[Compute], initialFeeRate[Compute])

	// step2: child complexity goes above target, so that average complexity is at target.
	// Fee rates go back to the original value
	m2 := &Manager{feeRates: m1.feeRates}
	require.NoError(m2.UpdateFeeRates(
		feesCfg,
		childComplexity,
		childBlkTime.Unix(),
		granChildBlkTime.Unix(),
	))

	require.Equal(initialFeeRate[Bandwidth], m2.feeRates[Bandwidth])
	require.Equal(initialFeeRate[UTXORead], m2.feeRates[UTXORead])
	require.Equal(initialFeeRate[UTXOWrite], m2.feeRates[UTXOWrite])
	require.Equal(initialFeeRate[Compute], m2.feeRates[Compute])
}
