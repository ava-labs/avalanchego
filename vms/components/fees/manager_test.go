// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/units"
)

type blkTimeAndComplexity struct {
	blkTime    int64
	complexity Dimensions
}

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
	require.Equal(uint64(21), m.feeRates[Bandwidth])

	// UTXORead complexity is at target, fee rate does not change
	require.Equal(parentFeeRate[UTXORead], m.feeRates[UTXORead])

	// UTXOWrite complexity is below target, fee rate is pushed down
	require.Equal(uint64(3), m.feeRates[UTXOWrite])

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
	require.Equal(uint64(0x55e63b88c6), m.feeRates[Compute])
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
			UpdateCoefficient:         Dimensions{20, 40, 50, 100},
			BlockTargetComplexityRate: Dimensions{25, 50, 100, 1000},
		}
		initialFeeRate = Dimensions{10, 100, 1_000, 1_000_000}

		elapsedTime      = time.Second
		parentComplexity = Dimensions{24, 45, 70, 500}   // less than target complexity rate * elapsedTime
		childComplexity  = Dimensions{26, 55, 130, 1500} // more than target complexity rate * elapsedTime

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

func TestPChainFeeRateIncreaseDueToPeak(t *testing.T) {
	// Complexity values comes from the mainnet historical peak as measured
	// pre E upgrade activation

	require := require.New(t)

	var (
		feesCfg = DynamicFeesConfig{
			MinFeeRate: Dimensions{
				60 * units.NanoAvax,
				8 * units.NanoAvax,
				10 * units.NanoAvax,
				35 * units.NanoAvax,
			},
			UpdateCoefficient: Dimensions{ // over CoeffDenom
				4,
				1,
				2,
				3,
			},
			BlockTargetComplexityRate: Dimensions{
				200,
				60,
				80,
				600,
			},
			BlockMaxComplexity: Dimensions{
				100_000,
				60_000,
				60_000,
				600_000,
			},
		}

		// See mainnet P-chain block 298vMuyYEi8R3XX6Ewi5orsDVYn5jrNDrPEaPG89UsckcN8e8K its descendants
		blockComplexities = []blkTimeAndComplexity{
			{1703455204, Dimensions{41388, 23040, 23122, 256000}},
			{1703455209, Dimensions{41388, 23040, 23122, 256000}},
			{1703455222, Dimensions{41388, 23040, 23122, 256000}},
			{1703455228, Dimensions{41388, 23040, 23122, 256000}},
			{1703455236, Dimensions{41388, 23040, 23122, 256000}},
			{1703455242, Dimensions{41388, 23040, 23122, 256000}},
			{1703455250, Dimensions{41388, 23040, 23122, 256000}},
			{1703455256, Dimensions{41388, 23040, 23122, 256000}},
			{1703455320, Dimensions{41388, 23040, 23122, 256000}},
			{1703455328, Dimensions{41388, 23040, 23122, 256000}},
			{1703455334, Dimensions{41388, 23040, 23122, 256000}},
			{1703455349, Dimensions{41388, 23040, 23122, 256000}},
			{1703455356, Dimensions{41388, 23040, 23122, 256000}},
			{1703455362, Dimensions{41388, 23040, 23122, 256000}},
			{1703455412, Dimensions{41388, 23040, 23122, 256000}},
			{1703455418, Dimensions{41388, 23040, 23122, 256000}},
			{1703455424, Dimensions{41388, 23040, 23122, 256000}},
			{1703455430, Dimensions{41388, 23040, 23122, 256000}},
			{1703455437, Dimensions{41388, 23040, 23122, 256000}},
			{1703455442, Dimensions{41388, 23040, 23122, 256000}},
			{1703455448, Dimensions{41388, 23040, 23122, 256000}},
			{1703455454, Dimensions{41388, 23040, 23122, 256000}},
			{1703455460, Dimensions{41388, 23040, 23122, 256000}},
			{1703455468, Dimensions{41388, 23040, 23122, 256000}},
			{1703455489, Dimensions{41388, 23040, 23122, 256000}},
			{1703455497, Dimensions{41388, 23040, 23122, 256000}},
			{1703455503, Dimensions{41388, 23040, 23122, 256000}},
			{1703455509, Dimensions{41388, 23040, 23122, 256000}},
			{1703455517, Dimensions{41388, 23040, 23122, 256000}},
			{1703455528, Dimensions{41388, 23040, 23122, 256000}},
		}
	)

	m := &Manager{
		feeRates: feesCfg.MinFeeRate,
	}

	// PEAK INCOMING
	peakFeeRate := feesCfg.MinFeeRate
	for i := 1; i < len(blockComplexities); i++ {
		parentBlkData := blockComplexities[i-1]
		childBlkData := blockComplexities[i]
		require.NoError(m.UpdateFeeRates(
			feesCfg,
			parentBlkData.complexity,
			parentBlkData.blkTime,
			childBlkData.blkTime,
		))

		// check that at least a fee rate component has strictly increased
		require.False(Compare(m.feeRates, peakFeeRate))

		// at peak the total fee for a median complexity tx should be in tens of Avax, no more.
		fee, err := m.CalculateFee(feesCfg.BlockTargetComplexityRate)
		require.NoError(err)
		require.Less(fee, 100*units.Avax, fmt.Sprintf("iteration: %d, total: %d", i, len(blockComplexities)))

		peakFeeRate = m.feeRates
	}

	// OFF PEAK
	offPeakBlkComplexity := Dimensions{
		feesCfg.BlockTargetComplexityRate[Bandwidth] * 1 / 10,
		feesCfg.BlockTargetComplexityRate[UTXORead] * 1 / 10,
		feesCfg.BlockTargetComplexityRate[UTXOWrite] * 1 / 10,
		feesCfg.BlockTargetComplexityRate[Compute] * 1 / 10,
	}
	elapsedTime := time.Second
	parentBlkTime := time.Now().Truncate(time.Second)
	childBlkTime := parentBlkTime.Add(elapsedTime)

	require.NoError(m.UpdateFeeRates(
		feesCfg,
		offPeakBlkComplexity,
		parentBlkTime.Unix(),
		childBlkTime.Unix(),
	))

	// fee rates must be strictly smaller than peak ones
	require.Less(m.feeRates[Bandwidth], peakFeeRate[Bandwidth])
	require.Less(m.feeRates[UTXORead], peakFeeRate[UTXORead])
	require.Less(m.feeRates[UTXOWrite], peakFeeRate[UTXOWrite])
	require.Less(m.feeRates[Compute], peakFeeRate[Compute])
}
