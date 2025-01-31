// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testMinBaseFee = big.NewInt(75_000_000_000)

type blockDefinition struct {
	timestamp uint64
	gasUsed   uint64
}

type test struct {
	baseFee   *big.Int
	genBlocks func() []blockDefinition
	minFee    *big.Int
}

func TestDynamicFees(t *testing.T) {
	spacedTimestamps := []uint64{1, 1, 2, 5, 15, 120}

	var tests []test = []test{
		// Test minimal gas usage
		{
			baseFee: nil,
			minFee:  testMinBaseFee,
			genBlocks: func() []blockDefinition {
				blocks := make([]blockDefinition, 0, len(spacedTimestamps))
				for _, timestamp := range spacedTimestamps {
					blocks = append(blocks, blockDefinition{
						timestamp: timestamp,
						gasUsed:   21000,
					})
				}
				return blocks
			},
		},
		// Test overflow handling
		{
			baseFee: nil,
			minFee:  testMinBaseFee,
			genBlocks: func() []blockDefinition {
				blocks := make([]blockDefinition, 0, len(spacedTimestamps))
				for _, timestamp := range spacedTimestamps {
					blocks = append(blocks, blockDefinition{
						timestamp: timestamp,
						gasUsed:   math.MaxUint64,
					})
				}
				return blocks
			},
		},
		// Test update increase handling
		{
			baseFee: big.NewInt(50_000_000_000),
			minFee:  testMinBaseFee,
			genBlocks: func() []blockDefinition {
				blocks := make([]blockDefinition, 0, len(spacedTimestamps))
				for _, timestamp := range spacedTimestamps {
					blocks = append(blocks, blockDefinition{
						timestamp: timestamp,
						gasUsed:   math.MaxUint64,
					})
				}
				return blocks
			},
		},
		{
			baseFee: nil,
			minFee:  testMinBaseFee,
			genBlocks: func() []blockDefinition {
				return []blockDefinition{
					{
						timestamp: 1,
						gasUsed:   1_000_000,
					},
					{
						timestamp: 3,
						gasUsed:   1_000_000,
					},
					{
						timestamp: 5,
						gasUsed:   2_000_000,
					},
					{
						timestamp: 5,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 7,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 1000,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 1001,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 1002,
						gasUsed:   6_000_000,
					},
				}
			},
		},
	}

	for _, test := range tests {
		testDynamicFeesStaysWithinRange(t, test)
	}
}

func testDynamicFeesStaysWithinRange(t *testing.T, test test) {
	blocks := test.genBlocks()
	initialBlock := blocks[0]
	header := &types.Header{
		Time:    initialBlock.timestamp,
		GasUsed: initialBlock.gasUsed,
		Number:  big.NewInt(0),
		BaseFee: test.baseFee,
	}

	for index, block := range blocks[1:] {
		testFeeConfig := commontype.FeeConfig{
			GasLimit:        big.NewInt(8_000_000),
			TargetBlockRate: 2, // in seconds

			MinBaseFee:               test.minFee,
			TargetGas:                big.NewInt(15_000_000),
			BaseFeeChangeDenominator: big.NewInt(36),

			MinBlockGasCost:  big.NewInt(0),
			MaxBlockGasCost:  big.NewInt(1_000_000),
			BlockGasCostStep: big.NewInt(200_000),
		}

		nextExtraData, nextBaseFee, err := CalcBaseFee(params.TestChainConfig, testFeeConfig, header, block.timestamp)
		if err != nil {
			t.Fatalf("Failed to calculate base fee at index %d: %s", index, err)
		}
		if nextBaseFee.Cmp(test.minFee) < 0 {
			t.Fatalf("Expected fee to stay greater than %d, but found %d", test.minFee, nextBaseFee)
		}
		log.Info("Update", "baseFee", nextBaseFee)
		header = &types.Header{
			Time:    block.timestamp,
			GasUsed: block.gasUsed,
			Number:  big.NewInt(int64(index) + 1),
			BaseFee: nextBaseFee,
			Extra:   nextExtraData,
		}
	}
}

func TestSelectBigWithinBounds(t *testing.T) {
	type test struct {
		lower, value, upper, expected *big.Int
	}

	tests := map[string]test{
		"value within bounds": {
			lower:    big.NewInt(0),
			value:    big.NewInt(5),
			upper:    big.NewInt(10),
			expected: big.NewInt(5),
		},
		"value below lower bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(-1),
			upper:    big.NewInt(10),
			expected: big.NewInt(0),
		},
		"value above upper bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(11),
			upper:    big.NewInt(10),
			expected: big.NewInt(10),
		},
		"value matches lower bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(0),
			upper:    big.NewInt(10),
			expected: big.NewInt(0),
		},
		"value matches upper bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(10),
			upper:    big.NewInt(10),
			expected: big.NewInt(10),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			v := selectBigWithinBounds(test.lower, test.value, test.upper)
			if v.Cmp(test.expected) != 0 {
				t.Fatalf("Expected (%d), found (%d)", test.expected, v)
			}
		})
	}
}

func TestCalcBlockGasCost(t *testing.T) {
	tests := map[string]struct {
		parentBlockGasCost      *big.Int
		parentTime, currentTime uint64

		expected *big.Int
	}{
		"Nil parentBlockGasCost": {
			parentBlockGasCost: nil,
			parentTime:         1,
			currentTime:        1,
			expected:           params.DefaultFeeConfig.MinBlockGasCost,
		},
		"Same timestamp from 0": {
			parentBlockGasCost: big.NewInt(0),
			parentTime:         1,
			currentTime:        1,
			expected:           big.NewInt(100_000),
		},
		"1s from 0": {
			parentBlockGasCost: big.NewInt(0),
			parentTime:         1,
			currentTime:        2,
			expected:           big.NewInt(50_000),
		},
		"Same timestamp from non-zero": {
			parentBlockGasCost: big.NewInt(50_000),
			parentTime:         1,
			currentTime:        1,
			expected:           big.NewInt(150_000),
		},
		"0s Difference (MAX)": {
			parentBlockGasCost: big.NewInt(1_000_000),
			parentTime:         1,
			currentTime:        1,
			expected:           big.NewInt(1_000_000),
		},
		"1s Difference (MAX)": {
			parentBlockGasCost: big.NewInt(1_000_000),
			parentTime:         1,
			currentTime:        2,
			expected:           big.NewInt(1_000_000),
		},
		"2s Difference": {
			parentBlockGasCost: big.NewInt(900_000),
			parentTime:         1,
			currentTime:        3,
			expected:           big.NewInt(900_000),
		},
		"3s Difference": {
			parentBlockGasCost: big.NewInt(1_000_000),
			parentTime:         1,
			currentTime:        4,
			expected:           big.NewInt(950_000),
		},
		"10s Difference": {
			parentBlockGasCost: big.NewInt(1_000_000),
			parentTime:         1,
			currentTime:        11,
			expected:           big.NewInt(600_000),
		},
		"20s Difference": {
			parentBlockGasCost: big.NewInt(1_000_000),
			parentTime:         1,
			currentTime:        21,
			expected:           big.NewInt(100_000),
		},
		"22s Difference": {
			parentBlockGasCost: big.NewInt(1_000_000),
			parentTime:         1,
			currentTime:        23,
			expected:           big.NewInt(0),
		},
		"23s Difference": {
			parentBlockGasCost: big.NewInt(1_000_000),
			parentTime:         1,
			currentTime:        24,
			expected:           big.NewInt(0),
		},
		"-1s Difference": {
			parentBlockGasCost: big.NewInt(50_000),
			parentTime:         1,
			currentTime:        0,
			expected:           big.NewInt(150_000),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Zero(t, test.expected.Cmp(calcBlockGasCost(
				params.DefaultFeeConfig.TargetBlockRate,
				params.DefaultFeeConfig.MinBlockGasCost,
				params.DefaultFeeConfig.MaxBlockGasCost,
				testBlockGasCostStep,
				test.parentBlockGasCost,
				test.parentTime,
				test.currentTime,
			)))
		})
	}
}

func TestCalcBaseFeeRegression(t *testing.T) {
	parentTimestamp := uint64(1)
	timestamp := parentTimestamp + params.RollupWindow + 1000

	parentHeader := &types.Header{
		Time:    parentTimestamp,
		GasUsed: 1_000_000,
		Number:  big.NewInt(1),
		BaseFee: big.NewInt(1),
		Extra:   make([]byte, params.DynamicFeeExtraDataSize),
	}

	testFeeConfig := commontype.FeeConfig{
		GasLimit:        big.NewInt(8_000_000),
		TargetBlockRate: 2, // in seconds

		MinBaseFee:               big.NewInt(1 * params.GWei),
		TargetGas:                big.NewInt(15_000_000),
		BaseFeeChangeDenominator: big.NewInt(100000),

		MinBlockGasCost:  big.NewInt(0),
		MaxBlockGasCost:  big.NewInt(1_000_000),
		BlockGasCostStep: big.NewInt(200_000),
	}
	_, _, err := CalcBaseFee(params.TestChainConfig, testFeeConfig, parentHeader, timestamp)
	require.NoError(t, err)
	require.Equalf(t, 0, common.Big1.Cmp(big.NewInt(1)), "big1 should be 1, got %s", common.Big1)
}
