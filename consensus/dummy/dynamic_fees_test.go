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

		nextExtraData, err := CalcExtraPrefix(params.TestChainConfig, testFeeConfig, header, block.timestamp)
		if err != nil {
			t.Fatalf("Failed to calculate extra prefix at index %d: %s", index, err)
		}
		nextBaseFee, err := CalcBaseFee(params.TestChainConfig, testFeeConfig, header, block.timestamp)
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
	_, err := CalcBaseFee(params.TestChainConfig, testFeeConfig, parentHeader, timestamp)
	require.NoError(t, err)
	require.Equalf(t, 0, common.Big1.Cmp(big.NewInt(1)), "big1 should be 1, got %s", common.Big1)
}

func TestEstimateNextBaseFee(t *testing.T) {
	testBaseFee := uint64(225 * params.GWei)
	nilUpgrade := params.NetworkUpgrades{}
	tests := []struct {
		name string

		upgrades params.NetworkUpgrades

		parentTime           uint64
		parentNumber         int64
		parentExtra          []byte
		parentBaseFee        *big.Int
		parentGasUsed        uint64
		parentExtDataGasUsed *big.Int

		timestamp uint64

		want    *big.Int
		wantErr error
	}{
		{
			name:          "activated",
			upgrades:      params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parentNumber:  1,
			parentExtra:   (&DynamicFeeWindow{}).Bytes(),
			parentBaseFee: new(big.Int).SetUint64(testBaseFee),
			timestamp:     1,
			want: func() *big.Int {
				var (
					gasTarget                  = testFeeConfig.TargetGas.Uint64()
					gasUsed                    = uint64(0)
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = testBaseFee
					smoothingFactor            = testFeeConfig.BaseFeeChangeDenominator.Uint64()
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					delta                      = baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return new(big.Int).SetUint64(baseFee)
			}(),
		},
		{
			name:     "not_scheduled",
			upgrades: nilUpgrade,
			wantErr:  errEstimateBaseFeeWithoutActivation,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			parentHeader := &types.Header{
				Time:    test.parentTime,
				Number:  big.NewInt(test.parentNumber),
				Extra:   test.parentExtra,
				BaseFee: test.parentBaseFee,
				GasUsed: test.parentGasUsed,
			}

			got, err := EstimateNextBaseFee(config, testFeeConfig, parentHeader, test.timestamp)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}
