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

func TestUpdateFeeRates(t *testing.T) {
	require := require.New(t)

	var (
		feesCfg = DynamicFeesConfig{
			MinFeeRate:                Dimensions{1, 1, 1, 1},
			UpdateDenominators:        Dimensions{100, 200, 500, 1_000},
			BlockMaxComplexity:        Dimensions{100, 100, 100, 100},
			BlockTargetComplexityRate: Dimensions{25, 25, 25, 25},
		}
		parentFeeRate    = Dimensions{10, 20, 100, 200}
		parentComplexity = Dimensions{100, 25, 20, 10}

		elapsedTime   = time.Second
		parentBlkTime = time.Now().Truncate(time.Second)
		childBlkTime  = parentBlkTime.Add(elapsedTime)
	)

	m, err := NewUpdatedManager(
		feesCfg,
		parentComplexity,
		parentBlkTime.Unix(),
		childBlkTime.Unix(),
	)
	require.NoError(err)

	// Bandwidth complexity are above target, fee rate is pushed up
	require.Equal(uint64(420), m.feeRates[Bandwidth])

	// UTXORead complexity is at target, fee rate does not change
	require.Equal(parentFeeRate[UTXORead], m.feeRates[UTXORead])

	// UTXOWrite complexity is below target, fee rate is pushed down
	require.Equal(uint64(33), m.feeRates[UTXOWrite])

	// Compute complexoty is below target, fee rate is pushed down
	require.Equal(uint64(1), m.feeRates[Compute])
}

// func TestFeeUpdateFactor(t *testing.T) {
// 	tests := []struct {
// 		coeff               uint64
// 		parentBlkComplexity uint64
// 		targetBlkComplexity uint64
// 		want                uint64
// 		wantIncreaseFee     bool
// 	}{
// 		// parentBlkComplexity == targetBlkComplexity gives factor 1, no matter what coeff is
// 		{1, 250, 250, 1, false},
// 		{math.MaxUint64, 250, 250, 1, false},

// 		// parentBlkComplexity > targetBlkComplexity
// 		{1, 101, 100, 1, true},      // should be   1.0005
// 		{1, 110, 100, 1, true},      // should be   1.005
// 		{1, 200, 100, 1, true},      // should be   1.05
// 		{1, 1_100, 100, 1, true},    // should be   1.648
// 		{1, 2_100, 100, 2, true},    // should be   2,718
// 		{1, 3_100, 100, 4, true},    // should be   4,48
// 		{1, 4_100, 100, 7, true},    // should be   7,39
// 		{1, 7_100, 100, 33, true},   // should be  33,12
// 		{1, 8_100, 100, 54, true},   // should be  54,6
// 		{1, 10_100, 100, 148, true}, // should be 148,4

// 		// parentBlkComplexity < targetBlkComplexity
// 		{1, 100, 101, 1, false},
// 		{1, 100, 110, 1, false},
// 		{1, 100, 200, 1, false},
// 		{1, 100, 1_100, 1, false},
// 		{1, 100, 2_100, 2, false},
// 		{1, 100, 3_100, 4, false},
// 		{1, 100, 4_100, 7, false},
// 		{1, 100, 7_100, 33, false},
// 		{1, 100, 8_100, 54, false},
// 		{1, 100, 10_100, 148, false},
// 	}
// 	for _, tt := range tests {
// 		haveFactor, haveIncreaseFee := updateFactor(
// 			tt.coeff,
// 			tt.parentBlkComplexity,
// 			tt.targetBlkComplexity,
// 		)
// 		require.Equal(t, tt.want, haveFactor)
// 		require.Equal(t, tt.wantIncreaseFee, haveIncreaseFee)
// 	}
// }

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
			UpdateDenominators: Dimensions{ // over CoeffDenom
				1,
				1,
				1,
				1,
			},
			BlockTargetComplexityRate: Dimensions{
				2500,
				600,
				1200,
				6500,
			},
			BlockMaxComplexity: Dimensions{
				100_000,
				60_000,
				60_000,
				600_000,
			},
		}

		// See mainnet P-chain block 2LJVD1rfEfaJtTwRggFXaUXhME4t5WYGhYP9Aj7eTYqGsfknuC its descendants
		blockComplexities = []struct {
			blkTime    int64
			complexity Dimensions
		}{
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
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
			// {1615237936, Dimensions{34064, 13056, 13056, 128000}},
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
		m, err := NewUpdatedManager(
			feesCfg,
			parentBlkData.complexity,
			parentBlkData.blkTime,
			childBlkData.blkTime,
		)
		require.NoError(err)

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

	m, err := NewUpdatedManager(
		feesCfg,
		offPeakBlkComplexity,
		parentBlkTime.Unix(),
		childBlkTime.Unix(),
	)
	require.NoError(err)

	// check that fee rates decrease off peak
	require.Less(m.feeRates[Bandwidth], peakFeeRate[Bandwidth])
	require.Less(m.feeRates[UTXORead], peakFeeRate[UTXORead])
	require.LessOrEqual(m.feeRates[UTXOWrite], peakFeeRate[UTXOWrite])
	require.Less(m.feeRates[Compute], peakFeeRate[Compute])
}
