// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/etna"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
)

func TestBaseFee(t *testing.T) {
	tests := []struct {
		name     string
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		timeMS   uint64
		want     *big.Int
		wantErr  error
	}{
		{
			name:     "ap2",
			upgrades: extras.TestApricotPhase2Config.NetworkUpgrades,
			want:     nil,
			wantErr:  nil,
		},
		{
			name: "ap3_first_block",
			upgrades: extras.NetworkUpgrades{
				ApricotPhase3BlockTimestamp: utils.NewUint64(1),
			},
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			timeMS: 1000,
			want:   big.NewInt(ap3.InitialBaseFee),
		},
		{
			name:     "ap3_genesis_block",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: big.NewInt(ap3.InitialBaseFee),
		},
		{
			name:     "ap3_invalid_fee_window",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			wantErr: ap3.ErrWindowInsufficientLength,
		},
		{
			name:     "ap3_invalid_timestamp",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Time:   1,
				Extra:  (&ap3.Window{}).Bytes(),
			},
			wantErr: errInvalidTimestamp,
		},
		{
			name:     "ap3_no_change",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: ap3.TargetGas - ap3.IntrinsicBlockGas,
				Time:    1,
				Extra:   (&ap3.Window{}).Bytes(),
				BaseFee: big.NewInt(ap3.MinBaseFee + 1),
			},
			timeMS: 1000,
			want:   big.NewInt(ap3.MinBaseFee + 1),
		},
		{
			name:     "ap3_small_decrease",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				Extra:   (&ap3.Window{}).Bytes(),
				BaseFee: big.NewInt(ap3.MaxBaseFee),
			},
			timeMS: 1000,
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
			name:     "ap3_large_decrease",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				Extra:   (&ap3.Window{}).Bytes(),
				BaseFee: big.NewInt(ap3.MaxBaseFee),
			},
			timeMS: 1000 * 2 * ap3.WindowLen,
			want: func() *big.Int {
				const (
					gasTarget                  = ap3.TargetGas
					gasUsed                    = 0
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = ap3.MaxBaseFee
					smoothingFactor            = ap3.BaseFeeChangeDenominator
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					windowsElapsed             = 2
					delta                      = windowsElapsed * baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "ap3_increase",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: 2 * ap3.TargetGas,
				Extra:   (&ap3.Window{}).Bytes(),
				BaseFee: big.NewInt(ap3.MinBaseFee),
			},
			timeMS: 1000,
			want: func() *big.Int {
				const (
					gasTarget                 = ap3.TargetGas
					gasUsed                   = 2*ap3.TargetGas + ap3.IntrinsicBlockGas
					amountOverTarget          = gasUsed - gasTarget
					parentBaseFee             = ap3.MinBaseFee
					smoothingFactor           = ap3.BaseFeeChangeDenominator
					baseFeeFractionOverTarget = amountOverTarget * parentBaseFee / gasTarget
					delta                     = baseFeeFractionOverTarget / smoothingFactor
					baseFee                   = parentBaseFee + delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "ap3_big_1_not_modified",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: 1,
				Extra:   (&ap3.Window{}).Bytes(),
				BaseFee: big.NewInt(1),
			},
			timeMS: 1000 * 2 * ap3.WindowLen,
			want:   big.NewInt(ap3.MinBaseFee),
		},
		{
			name:     "ap4_genesis_block",
			upgrades: extras.TestApricotPhase4Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: big.NewInt(ap3.InitialBaseFee),
		},
		{
			name:     "ap4_decrease",
			upgrades: extras.TestApricotPhase4Config.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(
				&types.Header{
					Number:  big.NewInt(1),
					Extra:   (&ap3.Window{}).Bytes(),
					BaseFee: big.NewInt(ap4.MaxBaseFee),
				},
				&customtypes.HeaderExtra{
					BlockGasCost: big.NewInt(ap4.MinBlockGasCost),
				},
			),
			timeMS: 1000,
			want: func() *big.Int {
				const (
					gasTarget                  = ap3.TargetGas
					gasUsed                    = (ap4.TargetBlockRate - 1) * ap4.BlockGasCostStep
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = ap4.MaxBaseFee
					smoothingFactor            = ap3.BaseFeeChangeDenominator
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					delta                      = baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "ap4_increase",
			upgrades: extras.TestApricotPhase4Config.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(
				&types.Header{
					Number:  big.NewInt(1),
					GasUsed: ap3.TargetGas,
					Extra:   (&ap3.Window{}).Bytes(),
					BaseFee: big.NewInt(ap4.MinBaseFee),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(ap3.TargetGas),
					BlockGasCost:   big.NewInt(ap4.MinBlockGasCost),
				},
			),
			timeMS: 1000,
			want: func() *big.Int {
				const (
					gasTarget                 = ap3.TargetGas
					gasUsed                   = 2*ap3.TargetGas + (ap4.TargetBlockRate-1)*ap4.BlockGasCostStep
					amountOverTarget          = gasUsed - gasTarget
					parentBaseFee             = ap4.MinBaseFee
					smoothingFactor           = ap3.BaseFeeChangeDenominator
					baseFeeFractionOverTarget = amountOverTarget * parentBaseFee / gasTarget
					delta                     = baseFeeFractionOverTarget / smoothingFactor
					baseFee                   = parentBaseFee + delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "ap5_genesis_block",
			upgrades: extras.TestApricotPhase5Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: big.NewInt(ap3.InitialBaseFee),
		},
		{
			name:     "ap5_decrease",
			upgrades: extras.TestApricotPhase5Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				Extra:   (&ap3.Window{}).Bytes(),
				BaseFee: big.NewInt(ap4.MaxBaseFee),
			},
			timeMS: 1000,
			want: func() *big.Int {
				const (
					gasTarget                  = ap5.TargetGas
					gasUsed                    = 0
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = ap4.MaxBaseFee
					smoothingFactor            = ap5.BaseFeeChangeDenominator
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					delta                      = baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "ap5_increase",
			upgrades: extras.TestApricotPhase5Config.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(
				&types.Header{
					Number:  big.NewInt(1),
					GasUsed: ap5.TargetGas,
					Extra:   (&ap3.Window{}).Bytes(),
					BaseFee: big.NewInt(ap4.MinBaseFee),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(ap5.TargetGas),
				},
			),
			timeMS: 1000,
			want: func() *big.Int {
				const (
					gasTarget                 = ap5.TargetGas
					gasUsed                   = 2 * ap5.TargetGas
					amountOverTarget          = gasUsed - gasTarget
					parentBaseFee             = ap4.MinBaseFee
					smoothingFactor           = ap5.BaseFeeChangeDenominator
					baseFeeFractionOverTarget = amountOverTarget * parentBaseFee / gasTarget
					delta                     = baseFeeFractionOverTarget / smoothingFactor
					baseFee                   = parentBaseFee + delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "etna_genesis_block",
			upgrades: extras.TestEtnaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: big.NewInt(ap3.InitialBaseFee),
		},
		{
			name:     "etna_increase",
			upgrades: extras.TestEtnaChainConfig.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(
				&types.Header{
					Number:  big.NewInt(1),
					GasUsed: ap5.TargetGas,
					Extra:   (&ap3.Window{}).Bytes(),
					BaseFee: big.NewInt(etna.MinBaseFee),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(ap5.TargetGas),
				},
			),
			timeMS: 1000,
			want: func() *big.Int {
				const (
					gasTarget                 = ap5.TargetGas
					gasUsed                   = 2 * ap5.TargetGas
					amountOverTarget          = gasUsed - gasTarget
					parentBaseFee             = etna.MinBaseFee
					smoothingFactor           = ap5.BaseFeeChangeDenominator
					baseFeeFractionOverTarget = amountOverTarget * parentBaseFee / gasTarget
					delta                     = baseFeeFractionOverTarget / smoothingFactor
					baseFee                   = parentBaseFee + delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "fortuna_invalid_timestamp",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Time:   1,
				Extra:  (&acp176.State{}).Bytes(),
			},
			timeMS:  0,
			wantErr: errInvalidTimestamp,
		},
		{
			name: "fortuna_first_block",
			upgrades: extras.NetworkUpgrades{
				FortunaTimestamp: utils.NewUint64(1),
			},
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			timeMS: 1000,
			want:   big.NewInt(acp176.MinGasPrice),
		},
		{
			name:     "fortuna_genesis_block",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: big.NewInt(acp176.MinGasPrice),
		},
		{
			name:     "fortuna_invalid_fee_state",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra:  make([]byte, acp176.StateSize-1),
			},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_current",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			want: big.NewInt(1_000_000_002), // nAVAX + 2 due to rounding
		},
		{
			name:     "fortuna_decrease",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			timeMS: 1000,
			want:   big.NewInt(988_571_555), // e^((2_704_386_192 - 1_500_000) / 1_500_000 / [acp176.TargetToPriceUpdateConversion])
		},
		{
			name:     "granite_invalid_timestamp",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(&types.Header{
				Number: big.NewInt(1),
				Time:   1,
				Extra:  (&acp176.State{}).Bytes(),
			}, &customtypes.HeaderExtra{
				TimeMilliseconds: utils.NewUint64(1000),
			}),
			timeMS:  0,
			wantErr: errInvalidTimestamp,
		},
		{
			name: "granite_first_block_with_state",
			upgrades: extras.NetworkUpgrades{
				FortunaTimestamp: utils.NewUint64(1),
				GraniteTimestamp: utils.NewUint64(1),
			},
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra:  (&acp176.State{}).Bytes(),
			},
			timeMS: 1000,
			want:   big.NewInt(acp176.MinGasPrice),
		},
		{
			name: "granite_first_block_after_fortuna",
			upgrades: extras.NetworkUpgrades{
				FortunaTimestamp: utils.NewUint64(0),
				GraniteTimestamp: utils.NewUint64(1),
			},
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			timeMS: 1000,
			want:   big.NewInt(988_571_555), // e^((2_704_386_192 - 1_500_000) / 1_500_000 / [acp176.TargetToPriceUpdateConversion])
		},
		{
			name:     "granite_genesis_block",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: big.NewInt(acp176.MinGasPrice),
		},
		{
			name:     "granite_invalid_fee_state",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra:  make([]byte, acp176.StateSize-1),
			},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "granite_decrease",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			timeMS: 1000,
			want:   big.NewInt(988_571_555), // e^((2_704_386_192 - 1_500_000) / 1_500_000 / [acp176.TargetToPriceUpdateConversion])
		},
		{
			name:     "granite_partial_second",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			timeMS: 500,
			want:   big.NewInt(994_269_358), // e^((2_704_386_192 - 750_000) / 1_500_000 / [acp176.TargetToPriceUpdateConversion])
		},
		{
			name:     "granite_1.25_seconds",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			timeMS: 1250,
			want:   big.NewInt(985_734_910), // e^((2_704_386_192 - 1_875_000) / 1_500_000 / [acp176.TargetToPriceUpdateConversion])
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &extras.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := BaseFee(config, test.parent, test.timeMS)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)

			// Verify that [common.Big1] is not modified by [BaseFee].
			require.Equal(big.NewInt(1), common.Big1)
		})
	}
}

func TestEstimateNextBaseFee(t *testing.T) {
	tests := []struct {
		name     string
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		timeMS   uint64
		want     *big.Int
	}{
		{
			name:     "fortuna",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			timeMS: 1000,
			want:   big.NewInt(988_571_555), // e^((2_704_386_192 - 1_500_000) / 1_500_000 / [acp176.TargetToPriceUpdateConversion])
		},
		{
			name:     "fortuna_milliseconds",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			timeMS: 1500,
			want:   big.NewInt(988_571_555), // same as above, rounded down.
		},
		{
			name:     "granite",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			timeMS: 1000,
			want:   big.NewInt(988_571_555), // e^((2_704_386_192 - 750_000) / 1_500_000 / [acp176.TargetToPriceUpdateConversion])
		},
		{
			name:     "granite_milliseconds",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Excess: 2_704_386_192, // 1_500_000 * ln(nAVAX) * [acp176.TargetToPriceUpdateConversion]
					},
					TargetExcess: 13_605_152, // 2^25 * ln(1.5)
				}).Bytes(),
			},
			timeMS: 1500,
			want:   big.NewInt(982_906_404), // e^((2_704_386_192 - 2_250_000) / 1_500_000 / [acp176.TargetToPriceUpdateConversion])
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &extras.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := EstimateNextBaseFee(config, test.parent, test.timeMS)
			require.NoError(err)
			require.Equal(test.want, got)
		})
	}
}
