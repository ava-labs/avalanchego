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
			UpdateCoefficient:         Dimensions{10, 20, 50, 100},
			BlockMaxComplexity:        Dimensions{100, 100, 100, 100},
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
	require.Equal(uint64(98), m.feeRates[UTXOWrite])

	// Compute complexoty is below target, fee rate is pushed down
	require.Equal(uint64(178), m.feeRates[Compute])
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
			BlockMaxComplexity:        Dimensions{100_000, 100_000, 100_000, 100_000},
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
				5,
				2,
				2,
				4,
			},
			BlockTargetComplexityRate: Dimensions{
				250,
				60,
				120,
				650,
			},
			BlockMaxComplexity: Dimensions{
				100_000,
				60_000,
				60_000,
				600_000,
			},
		}

		// See mainnet P-chain block 2LJVD1rfEfaJtTwRggFXaUXhME4t5WYGhYP9Aj7eTYqGsfknuC its descendants
		blockComplexities = []blkTimeAndComplexity{
			{1615237936, Dimensions{28234, 10812, 10812, 106000}},
			{1615237936, Dimensions{17634, 6732, 6732, 66000}},
			{1615237936, Dimensions{12334, 4692, 4692, 46000}},
			{1615237936, Dimensions{5709, 2142, 2142, 21000}},
			{1615237936, Dimensions{15514, 5916, 5916, 58000}},
			{1615237936, Dimensions{12069, 4590, 4590, 45000}},
			{1615237936, Dimensions{8359, 3162, 3162, 31000}},
			{1615237936, Dimensions{5444, 2040, 2040, 20000}},
			{1615237936, Dimensions{1734, 612, 612, 6000}},
			{1615237936, Dimensions{5974, 2244, 2244, 22000}},
			{1615237936, Dimensions{3059, 1122, 1122, 11000}},
			{1615237936, Dimensions{7034, 2652, 2652, 26000}},
			{1615237936, Dimensions{7564, 2856, 2856, 28000}},
			{1615237936, Dimensions{34064, 13056, 13056, 128000}},
			{1615237936, Dimensions{34064, 13056, 13056, 128000}},
			{1615237936, Dimensions{34064, 13056, 13056, 128000}},
			{1615237936, Dimensions{34064, 13056, 13056, 128000}},
			{1615237936, Dimensions{34064, 13056, 13056, 128000}},
			{1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}}, <-- from here on, fee would exceed 100 Avax
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{820, 360, 442, 4000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{3589, 1326, 1326, 13000}},
			// {1615237936, Dimensions{550, 180, 180, 2000}},
			// {1615237936, Dimensions{413, 102, 102, 1000}},
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

		// check that fee rates are strictly above minimal
		require.False(
			Compare(m.feeRates, feesCfg.MinFeeRate),
			fmt.Sprintf("failed at %d of %d iteration, \n curr fees %v \n next fees %v",
				i,
				len(blockComplexities),
				peakFeeRate,
				m.feeRates,
			),
		)

		// at peak the total fee should be no more than 100 Avax.
		fee, err := m.CalculateFee(childBlkData.complexity)
		require.NoError(err)
		require.Less(fee, 100*units.Avax, fmt.Sprintf("iteration: %d, total: %d", i, len(blockComplexities)))

		peakFeeRate = m.feeRates
	}

	// OFF PEAK
	offPeakBlkComplexity := Dimensions{1473, 510, 510, 5000}
	elapsedTime := time.Unix(1615238881, 0).Sub(time.Unix(1615237936, 0))
	parentBlkTime := time.Now().Truncate(time.Second)
	childBlkTime := parentBlkTime.Add(elapsedTime)

	require.NoError(m.UpdateFeeRates(
		feesCfg,
		offPeakBlkComplexity,
		parentBlkTime.Unix(),
		childBlkTime.Unix(),
	))

	// check that fee rates decrease off peak
	require.Less(m.feeRates[Bandwidth], peakFeeRate[Bandwidth])
	require.Less(m.feeRates[UTXORead], peakFeeRate[UTXORead])
	require.LessOrEqual(m.feeRates[UTXOWrite], peakFeeRate[UTXOWrite])
	require.Less(m.feeRates[Compute], peakFeeRate[Compute])
}

func TestFeeUpdateFactor(t *testing.T) {
	tests := []struct {
		coeff               uint64
		parentBlkComplexity uint64
		targetBlkComplexity uint64
		wantNum             uint64
		wantDenom           uint64
	}{
		// parentBlkComplexity == targetBlkComplexity gives factor 1, no matter what coeff is
		{1, 250, 250, 1, 1},
		{math.MaxUint64, 250, 250, 1, 1},

		// PIECEWISE APPROXIMATION parentBlkComplexity > targetBlkComplexity
		{1, 101, 100, 100_002, 100_000},             // should be 1,00001
		{1, 110, 100, 100_020, 100_000},             // should be 1,0001
		{1, 200, 100, 100_200, 100_000},             // should be 1,001
		{1, 1_100, 100, 102_000, 100_000},           // should be 1,01
		{1, 10_100, 100, 120_000, 100_000},          // should be 1,105
		{1, 100_100, 100, 300_000, 100_000},         // should be 2,718, it's 3
		{1, 1_000_100, 100, 2_202_800_000, 100_000}, // should be 22026, it's 22028

		// // TAYLOR APPROXIMATION parentBlkComplexity > targetBlkComplexity
		// {1, 101, 100, 20_000_200_001, 20_000_000_000},          // should be 1,00001
		// {1, 110, 100, 20_002_000_100, 20_000_000_000},          // should be 1,0001
		// {1, 200, 100, 20_020_010_000, 20_000_000_000},          // should be 1,001
		// {1, 1_100, 100, 20_201_000_000, 20_000_000_000},        // should be 1,01
		// {1, 10_100, 100, 22_100_000_000, 20_000_000_000},       // should be 1,105
		// {1, 100_100, 100, 50_000_000_000, 20_000_000_000},      // should be 2,718, it's 2,5
		// {1, 1_000_100, 100, 1_220_000_000_000, 20_000_000_000}, // should be 22026, it's roughly 61

		// PIECEWISE APPROXIMATION parentBlkComplexity < targetBlkComplexity
		{1, 100, 101, 101_000, 101_002},                   // should be 0,99999
		{1, 100, 110, 110_000, 110_020},                   // should be 0,9999
		{1, 100, 200, 200_000, 200_200},                   // should be 0,9995
		{1, 100, 1_100, 1_100_000, 1_102_000},             // should be 0,9991
		{1, 100, 10_100, 10_100_000, 10_120_000},          // should be 0,999
		{1, 100, 100_100, 100_100_000, 100_300_000},       // should be 0,999
		{1, 100, 1_000_100, 1_000_100_000, 1_002_100_000}, // should be 0,999

		// // TAYLOR APPROXIMATION parentBlkComplexity < targetBlkComplexity
		// {1, 100, 101, 20_402_000_000, 20_402_202_001},                             // should be 0,99999
		// {1, 100, 110, 24_200_000_000, 24_202_200_100},                             // should be 0,9999
		// {1, 100, 200, 80_000_000_000, 80_040_010_000},                             // should be 0,9995
		// {1, 100, 1_100, 2_420_000_000_000, 2_422_201_000_000},                     // should be 0,9991
		// {1, 100, 10_100, 204_020_000_000_000, 204_222_100_000_000},                // should be 0,999
		// {1, 100, 100_100, 20_040_020_000_000_000, 20_060_050_000_000_000},         // should be 0,999
		// {1, 100, 1_000_100, 2_000_400_020_000_000_000, 2_002_401_220_000_000_000}, // should be 0,999
	}
	for _, tt := range tests {
		haveFactor, haveIncreaseFee := updateFactor(
			tt.coeff,
			tt.parentBlkComplexity,
			tt.targetBlkComplexity,
		)
		require.Equal(t, tt.wantNum, haveFactor)
		require.Equal(t, tt.wantDenom, haveIncreaseFee)
	}
}
