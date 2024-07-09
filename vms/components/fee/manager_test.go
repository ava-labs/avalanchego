// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/units"
)

var (
	testDynamicFeeCfg = DynamicFeesConfig{
		MinGasPrice:       GasPrice(60 * units.NanoAvax),
		UpdateDenominator: Gas(50_000),
		GasTargetRate:     Gas(250),

		FeeDimensionWeights: Dimensions{1, 1, 1, 1},
	}
	testGasCap = Gas(math.MaxUint64)
)

func TestUpdateGasPrice(t *testing.T) {
	require := require.New(t)

	var (
		parentGasPrice = GasPrice(10)
		excessGas      = Gas(300)

		elapsedTime   = time.Second
		parentBlkTime = time.Now().Truncate(time.Second)
		childBlkTime  = parentBlkTime.Add(elapsedTime)
	)

	m, err := NewUpdatedManager(
		testDynamicFeeCfg,
		testGasCap,
		excessGas,
		parentBlkTime,
		childBlkTime,
	)
	require.NoError(err)

	// Gas above target, gas price is pushed up
	require.Greater(m.GetGasPrice(), parentGasPrice)
}

func TestFakeExponential(t *testing.T) {
	tests := []struct {
		factor      GasPrice
		numerator   Gas
		denominator Gas
		want        GasPrice
	}{
		// When numerator == 0 the return value should always equal the value of factor
		{1, 0, 1, 1},
		{38493, 0, 1000, 38493},
		{0, 1234, 2345, 0}, // should be 0
		{1, 2, 1, 6},       // approximate 7.389
		{1, 4, 2, 6},
		{1, 3, 1, 16}, // approximate 20.09
		{1, 6, 2, 18},
		{1, 4, 1, 49}, // approximate 54.60
		{1, 8, 2, 50},
		{10, 8, 2, 542}, // approximate 540.598
		{11, 8, 2, 596}, // approximate 600.58
		{1, 5, 1, 136},  // approximate 148.4
		{1, 5, 2, 11},   // approximate 12.18
		{2, 5, 2, 23},   // approximate 24.36
		{1, 50000000, 2225652, 5709098764},
	}
	for _, tt := range tests {
		have := fakeExponential(tt.factor, tt.numerator, tt.denominator)
		require.Equal(t, tt.want, have)
	}
}

func TestPChainGasPriceIncreaseDueToPeak(t *testing.T) {
	// Complexity values comes from the mainnet historical peak as measured
	// pre E upgrade activation

	require := require.New(t)

	// See mainnet P-chain block 2LJVD1rfEfaJtTwRggFXaUXhME4t5WYGhYP9Aj7eTYqGsfknuC its descendants
	blockComplexities := []struct {
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
		{1615237936, Dimensions{34064, 13056, 13056, 128000}},
		{1615237936, Dimensions{34064, 13056, 13056, 128000}},
		{1615237936, Dimensions{34064, 13056, 13056, 128000}},
		{1615237936, Dimensions{34064, 13056, 13056, 128000}},
		{1615237936, Dimensions{34064, 13056, 13056, 128000}},
		{1615237936, Dimensions{34064, 13056, 13056, 128000}},
		{1615237936, Dimensions{34064, 13056, 13056, 128000}},
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

	// PEAK INCOMING
	peakGasPrice := testDynamicFeeCfg.MinGasPrice
	for i := 1; i < len(blockComplexities); i++ {
		parentBlkData := blockComplexities[i-1]
		childBlkData := blockComplexities[i]

		excessGas, err := ToGas(testDynamicFeeCfg.FeeDimensionWeights, parentBlkData.complexity)
		require.NoError(err)

		m, err := NewUpdatedManager(
			testDynamicFeeCfg,
			testGasCap,
			excessGas,
			time.Unix(parentBlkData.blkTime, 0),
			time.Unix(childBlkData.blkTime, 0),
		)
		require.NoError(err)

		// check that gas price is strictly above minimal
		require.Greater(m.GetGasPrice(), testDynamicFeeCfg.MinGasPrice,
			fmt.Sprintf("failed at %d of %d iteration, \n curr gas price %v \n next gas price %v",
				i,
				len(blockComplexities),
				peakGasPrice,
				m.GetGasPrice(),
			),
		)

		// at peak the total fee should be no more than 100 Avax.
		require.NoError(m.CumulateComplexity(childBlkData.complexity))
		fee, err := m.GetLatestTxFee()
		require.NoError(err)
		require.Less(fee, 100*units.Avax, fmt.Sprintf("iteration: %d, total: %d", i, len(blockComplexities)))
		require.NoError(m.DoneWithLatestTx())

		peakGasPrice = m.GetGasPrice()
	}

	// OFF PEAK
	offPeakBlkComplexity := Dimensions{1473, 510, 510, 5000}
	offPeakGas, err := ToGas(testDynamicFeeCfg.FeeDimensionWeights, offPeakBlkComplexity)
	require.NoError(err)

	elapsedTime := time.Unix(1615238881, 0).Sub(time.Unix(1615237936, 0))
	parentBlkTime := time.Now().Truncate(time.Second)
	childBlkTime := parentBlkTime.Add(elapsedTime)

	m, err := NewUpdatedManager(
		testDynamicFeeCfg,
		testGasCap,
		offPeakGas,
		parentBlkTime,
		childBlkTime,
	)
	require.NoError(err)

	// check that gas price decreases off peak
	require.Less(m.GetGasPrice(), peakGasPrice)
}
