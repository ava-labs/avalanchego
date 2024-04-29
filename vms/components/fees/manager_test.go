// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"
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
			UpdateCoefficient:         Dimensions{10, 20, 50, 100},
			BlockTargetComplexityRate: Dimensions{25, 25, 25, 25},
		}
		parentFeeRate    = Dimensions{10, 20, 100, 200}
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
	require.Equal(uint64(10), m.feeRates[Bandwidth])

	// UTXORead complexity is at target, fee rate does not change
	require.Equal(parentFeeRate[UTXORead], m.feeRates[UTXORead])

	// UTXOWrite complexity is below target, fee rate is pushed down
	require.Equal(uint64(99), m.feeRates[UTXOWrite])

	// Compute complexoty is below target, fee rate is pushed down to the minimum
	require.Equal(uint64(193), m.feeRates[Compute])
}

func TestUpdateFeeRatesStability(t *testing.T) {
	// The advantage of using an exponential fee update scheme
	// (vs e.g. the EIP-1559 scheme we use in the C-chain) is that
	// it is more stable against dithering.
	// We prove here that if complexity oscillates around the target
	// fee rates are unchanged (discounting for some numerical errors)

	require := require.New(t)

	var (
		feesCfg = DynamicFeesConfig{
			MinFeeRate:                Dimensions{0, 0, 0, 0},
			UpdateCoefficient:         Dimensions{2, 4, 5, 10},
			BlockTargetComplexityRate: Dimensions{200, 60, 80, 600},
		}
		initialFeeRate = Dimensions{
			60 * units.NanoAvax,
			8 * units.NanoAvax,
			10 * units.NanoAvax,
			35 * units.NanoAvax,
		}

		elapsedTime      = time.Second
		parentComplexity = Dimensions{50, 45, 70, 500}  // less than target complexity rate * elapsedTime
		childComplexity  = Dimensions{350, 75, 90, 700} // more than target complexity rate * elapsedTime

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

	require.Less(m1.feeRates[Bandwidth], initialFeeRate[Bandwidth])
	require.Less(m1.feeRates[UTXORead], initialFeeRate[UTXORead])
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

	require.LessOrEqual(initialFeeRate[Bandwidth]-m2.feeRates[Bandwidth], uint64(1))
	require.LessOrEqual(initialFeeRate[UTXORead]-m2.feeRates[UTXORead], uint64(1))
	require.LessOrEqual(initialFeeRate[UTXOWrite]-m2.feeRates[UTXOWrite], uint64(1))
	require.LessOrEqual(initialFeeRate[Compute]-m2.feeRates[Compute], uint64(1))
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
				4 * 200,
				4 * 60,
				4 * 80,
				4 * 600,
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
		require.False(
			Compare(m.feeRates, peakFeeRate),
			fmt.Sprintf("failed at %d of %d iteration, \n curr fees %v \n next fees %v",
				i,
				len(blockComplexities),
				peakFeeRate,
				m.feeRates,
			))

		// at peak the total fee for a median complexity tx should be in tens of Avax, no more.
		fee, err := m.CalculateFee(feesCfg.BlockTargetComplexityRate, NoTip)
		require.NoError(err)
		require.Less(fee, 100*units.Avax, fmt.Sprintf("iteration: %d, total: %d", i, len(blockComplexities)))

		peakFeeRate = m.feeRates
	}

	// OFF PEAK
	offPeakBlkComplexity := Dimensions{
		feesCfg.BlockTargetComplexityRate[Bandwidth] * 99 / 100,
		feesCfg.BlockTargetComplexityRate[UTXORead] * 99 / 100,
		feesCfg.BlockTargetComplexityRate[UTXOWrite] * 99 / 100,
		feesCfg.BlockTargetComplexityRate[Compute] * 99 / 100,
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
