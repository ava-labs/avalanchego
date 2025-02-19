// (c) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/ap3"
	"github.com/ava-labs/coreth/plugin/evm/ap4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockDefinition struct {
	timestamp      uint64
	gasUsed        uint64
	extDataGasUsed *big.Int
}

type test struct {
	blocks         []blockDefinition
	minFee, maxFee *big.Int
}

func TestDynamicFees(t *testing.T) {
	spacedTimestamps := []uint64{1, 1, 2, 5, 15, 120}

	tests := []test{
		// Test minimal gas usage
		{
			minFee: big.NewInt(ap3.MinBaseFee),
			maxFee: big.NewInt(ap3.MaxBaseFee),
			blocks: func() []blockDefinition {
				blocks := make([]blockDefinition, 0, len(spacedTimestamps))
				for _, timestamp := range spacedTimestamps {
					blocks = append(blocks, blockDefinition{
						timestamp: timestamp,
						gasUsed:   21000,
					})
				}
				return blocks
			}(),
		},
		// Test overflow handling
		{
			minFee: big.NewInt(ap3.MinBaseFee),
			maxFee: big.NewInt(ap3.MaxBaseFee),
			blocks: func() []blockDefinition {
				blocks := make([]blockDefinition, 0, len(spacedTimestamps))
				for _, timestamp := range spacedTimestamps {
					blocks = append(blocks, blockDefinition{
						timestamp: timestamp,
						gasUsed:   math.MaxUint64,
					})
				}
				return blocks
			}(),
		},
		{
			minFee: big.NewInt(ap3.MinBaseFee),
			maxFee: big.NewInt(ap3.MaxBaseFee),
			blocks: []blockDefinition{
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
			},
		},
	}

	for _, test := range tests {
		testDynamicFeesStaysWithinRange(t, test)
	}
}

func testDynamicFeesStaysWithinRange(t *testing.T, test test) {
	blocks := test.blocks
	initialBlock := blocks[0]
	header := &types.Header{
		Time:    initialBlock.timestamp,
		GasUsed: initialBlock.gasUsed,
		Number:  big.NewInt(0),
	}

	for index, block := range blocks[1:] {
		nextExtraData, err := ExtraPrefix(params.TestApricotPhase3Config, header, block.timestamp)
		if err != nil {
			t.Fatalf("Failed to calculate extra prefix at index %d: %s", index, err)
		}
		nextBaseFee, err := BaseFee(params.TestApricotPhase3Config, header, block.timestamp)
		if err != nil {
			t.Fatalf("Failed to calculate base fee at index %d: %s", index, err)
		}
		if nextBaseFee.Cmp(test.maxFee) > 0 {
			t.Fatalf("Expected fee to stay less than %d, but found %d", test.maxFee, nextBaseFee)
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

// TestCalcBaseFeeAP4 confirms that the inclusion of ExtDataGasUsage increases
// the base fee.
func TestCalcBaseFeeAP4(t *testing.T) {
	events := []struct {
		block             blockDefinition
		extDataFeeGreater bool
	}{
		{
			block: blockDefinition{
				timestamp:      1,
				gasUsed:        1_000_000,
				extDataGasUsed: big.NewInt(100_000),
			},
		},
		{
			block: blockDefinition{
				timestamp:      3,
				gasUsed:        1_000_000,
				extDataGasUsed: big.NewInt(10_000),
			},
			extDataFeeGreater: true,
		},
		{
			block: blockDefinition{
				timestamp:      5,
				gasUsed:        2_000_000,
				extDataGasUsed: big.NewInt(50_000),
			},
			extDataFeeGreater: true,
		},
		{
			block: blockDefinition{
				timestamp:      5,
				gasUsed:        6_000_000,
				extDataGasUsed: big.NewInt(50_000),
			},
			extDataFeeGreater: true,
		},
		{
			block: blockDefinition{
				timestamp:      7,
				gasUsed:        6_000_000,
				extDataGasUsed: big.NewInt(0),
			},
			extDataFeeGreater: true,
		},
		{
			block: blockDefinition{
				timestamp:      1000,
				gasUsed:        6_000_000,
				extDataGasUsed: big.NewInt(0),
			},
		},
		{
			block: blockDefinition{
				timestamp:      1001,
				gasUsed:        6_000_000,
				extDataGasUsed: big.NewInt(10_000),
			},
		},
		{
			block: blockDefinition{
				timestamp:      1002,
				gasUsed:        6_000_000,
				extDataGasUsed: big.NewInt(0),
			},
			extDataFeeGreater: true,
		},
	}

	header := &types.Header{
		Time:    0,
		GasUsed: 1_000_000,
		Number:  big.NewInt(0),
		BaseFee: big.NewInt(225 * params.GWei),
		Extra:   nil,
	}
	extDataHeader := &types.Header{
		Time:    0,
		GasUsed: 1_000_000,
		Number:  big.NewInt(0),
		BaseFee: big.NewInt(225 * params.GWei),
		Extra:   nil,
		// ExtDataGasUsage is set to be nil to ensure CalcBaseFee can handle the
		// AP3/AP4 boundary.
	}

	for index, event := range events {
		block := event.block
		nextExtraData, err := ExtraPrefix(params.TestApricotPhase4Config, header, block.timestamp)
		assert.NoError(t, err)
		nextBaseFee, err := BaseFee(params.TestApricotPhase4Config, header, block.timestamp)
		assert.NoError(t, err)
		log.Info("Update", "baseFee", nextBaseFee)
		header = &types.Header{
			Time:    block.timestamp,
			GasUsed: block.gasUsed,
			Number:  big.NewInt(int64(index) + 1),
			BaseFee: nextBaseFee,
			Extra:   nextExtraData,
		}

		nextExtraData, err = ExtraPrefix(params.TestApricotPhase4Config, extDataHeader, block.timestamp)
		assert.NoError(t, err)
		nextBaseFee, err = BaseFee(params.TestApricotPhase4Config, extDataHeader, block.timestamp)
		assert.NoError(t, err)
		log.Info("Update", "baseFee (w/extData)", nextBaseFee)
		extDataHeader = &types.Header{
			Time:           block.timestamp,
			GasUsed:        block.gasUsed,
			Number:         big.NewInt(int64(index) + 1),
			BaseFee:        nextBaseFee,
			Extra:          nextExtraData,
			ExtDataGasUsed: block.extDataGasUsed,
		}

		assert.Equal(t, event.extDataFeeGreater, extDataHeader.BaseFee.Cmp(header.BaseFee) == 1, "unexpected cmp for index %d", index)
	}
}

func TestDynamicFeesEtna(t *testing.T) {
	require := require.New(t)
	header := &types.Header{
		Number: big.NewInt(0),
	}

	timestamp := uint64(1)
	extra, err := ExtraPrefix(params.TestEtnaChainConfig, header, timestamp)
	require.NoError(err)
	nextBaseFee, err := BaseFee(params.TestEtnaChainConfig, header, timestamp)
	require.NoError(err)
	// Genesis matches the initial base fee
	require.Equal(int64(ap3.InitialBaseFee), nextBaseFee.Int64())

	timestamp = uint64(10_000)
	header = &types.Header{
		Number:  big.NewInt(1),
		Time:    header.Time,
		BaseFee: nextBaseFee,
		Extra:   extra,
	}
	nextBaseFee, err = BaseFee(params.TestEtnaChainConfig, header, timestamp)
	require.NoError(err)
	// After some time has passed in the Etna phase, the base fee should drop
	// lower than the prior base fee minimum.
	require.Less(nextBaseFee.Int64(), int64(ap4.MinBaseFee))
}

func TestCalcBaseFeeRegression(t *testing.T) {
	parentTimestamp := uint64(1)
	timestamp := parentTimestamp + ap3.WindowLen + 1000

	parentHeader := &types.Header{
		Time:    parentTimestamp,
		GasUsed: 14_999_999,
		Number:  big.NewInt(1),
		BaseFee: big.NewInt(1),
		Extra:   make([]byte, FeeWindowSize),
	}

	_, err := BaseFee(params.TestChainConfig, parentHeader, timestamp)
	require.NoError(t, err)
	require.Equalf(t, 0, common.Big1.Cmp(big.NewInt(1)), "big1 should be 1, got %s", common.Big1)
}

func TestEstimateNextBaseFee(t *testing.T) {
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
			name:          "ap3",
			upgrades:      params.TestApricotPhase3Config.NetworkUpgrades,
			parentNumber:  1,
			parentExtra:   feeWindowBytes(ap3.Window{}),
			parentBaseFee: big.NewInt(ap3.MaxBaseFee),
			timestamp:     1,
			want: func() *big.Int {
				const (
					gasTarget                  = ap3.TargetGas
					gasUsed                    = ap3.IntrinsicBlockGas
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = ap3.MaxBaseFee
					smoothingFactor            = ap3.BaseFeeChangeDenominator
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					delta                      = baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "ap3_not_scheduled",
			upgrades: params.TestApricotPhase2Config.NetworkUpgrades,
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
				Time:           test.parentTime,
				Number:         big.NewInt(test.parentNumber),
				Extra:          test.parentExtra,
				BaseFee:        test.parentBaseFee,
				GasUsed:        test.parentGasUsed,
				ExtDataGasUsed: test.parentExtDataGasUsed,
			}

			got, err := EstimateNextBaseFee(config, parentHeader, test.timestamp)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}
