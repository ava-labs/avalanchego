// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/upgrade/subnetevm"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils"
)

const (
	maxBaseFee = 225 * utils.GWei
)

func TestBaseFee(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		BaseFeeTest(t, testFeeConfig)
	})
	t.Run("double", func(t *testing.T) {
		BaseFeeTest(t, testFeeConfigDouble)
	})
}

func BaseFeeTest(t *testing.T, feeConfig commontype.FeeConfig) {
	tests := []struct {
		name     string
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		timeMS   uint64
		want     *big.Int
		wantErr  error
	}{
		{
			name:     "pre_subnet_evm",
			upgrades: extras.TestPreSubnetEVMChainConfig.NetworkUpgrades,
			want:     nil,
			wantErr:  nil,
		},
		{
			name: "subnet_evm_first_block",
			upgrades: extras.NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
			},
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			timeMS: 1000,
			want:   big.NewInt(feeConfig.MinBaseFee.Int64()),
		},
		{
			name:     "subnet_evm_genesis_block",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: big.NewInt(feeConfig.MinBaseFee.Int64()),
		},
		{
			name:     "subnet_evm_invalid_fee_window",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			wantErr: subnetevm.ErrWindowInsufficientLength,
		},
		{
			name:     "subnet_evm_invalid_timestamp",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Time:   1,
				Extra:  (&subnetevm.Window{}).Bytes(),
			},
			wantErr: errInvalidTimestamp,
		},
		{
			name:     "subnet_evm_no_change",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: feeConfig.TargetGas.Uint64(),
				Time:    1,
				Extra:   (&subnetevm.Window{}).Bytes(),
				BaseFee: big.NewInt(feeConfig.MinBaseFee.Int64() + 1),
			},
			timeMS: 1000,
			want:   big.NewInt(feeConfig.MinBaseFee.Int64() + 1),
		},
		{
			name:     "subnet_evm_small_decrease",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				Extra:   (&subnetevm.Window{}).Bytes(),
				BaseFee: big.NewInt(maxBaseFee),
			},
			timeMS: 1000,
			want: func() *big.Int {
				var (
					gasTarget                  = feeConfig.TargetGas.Int64()
					gasUsed                    = int64(0)
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = int64(maxBaseFee)
					smoothingFactor            = feeConfig.BaseFeeChangeDenominator.Int64()
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					delta                      = baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "subnet_evm_large_decrease",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				Extra:   (&subnetevm.Window{}).Bytes(),
				BaseFee: big.NewInt(maxBaseFee),
			},
			timeMS: 2 * 1000 * subnetevm.WindowLen,
			want: func() *big.Int {
				var (
					gasTarget                  = feeConfig.TargetGas.Int64()
					gasUsed                    = int64(0)
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = int64(maxBaseFee)
					smoothingFactor            = feeConfig.BaseFeeChangeDenominator.Int64()
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					windowsElapsed             = int64(2)
					delta                      = windowsElapsed * baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "subnet_evm_increase",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: 2 * feeConfig.TargetGas.Uint64(),
				Extra:   (&subnetevm.Window{}).Bytes(),
				BaseFee: big.NewInt(feeConfig.MinBaseFee.Int64()),
			},
			timeMS: 1000,
			want: func() *big.Int {
				var (
					gasTarget                 = feeConfig.TargetGas.Int64()
					gasUsed                   = 2 * gasTarget
					amountOverTarget          = gasUsed - gasTarget
					parentBaseFee             = feeConfig.MinBaseFee.Int64()
					smoothingFactor           = feeConfig.BaseFeeChangeDenominator.Int64()
					baseFeeFractionOverTarget = amountOverTarget * parentBaseFee / gasTarget
					delta                     = baseFeeFractionOverTarget / smoothingFactor
					baseFee                   = parentBaseFee + delta
				)
				return big.NewInt(baseFee)
			}(),
		},
		{
			name:     "subnet_evm_big_1_not_modified",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: 1,
				Extra:   (&subnetevm.Window{}).Bytes(),
				BaseFee: big.NewInt(1),
			},
			timeMS: 2 * 1000 * subnetevm.WindowLen,
			want:   big.NewInt(feeConfig.MinBaseFee.Int64()),
		},
		{
			name:     "granite_rounds_seconds",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				Extra:   (&subnetevm.Window{}).Bytes(),
				BaseFee: big.NewInt(maxBaseFee),
			},
			timeMS: 2*1000*subnetevm.WindowLen + 999,
			want: func() *big.Int {
				var (
					gasTarget                  = feeConfig.TargetGas.Int64()
					gasUsed                    = int64(0)
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = int64(maxBaseFee)
					smoothingFactor            = feeConfig.BaseFeeChangeDenominator.Int64()
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					windowsElapsed             = int64(2)
					delta                      = windowsElapsed * baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return big.NewInt(baseFee)
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &extras.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := BaseFee(config, feeConfig, test.parent, test.timeMS)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)

			// Verify that [common.Big1] is not modified by [BaseFee].
			require.Equal(big.NewInt(1), common.Big1)
		})
	}
}

func TestEstimateNextBaseFee(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		EstimateNextBaseFeeTest(t, testFeeConfig)
	})
	t.Run("double", func(t *testing.T) {
		EstimateNextBaseFeeTest(t, testFeeConfigDouble)
	})
}

func EstimateNextBaseFeeTest(t *testing.T, feeConfig commontype.FeeConfig) {
	testBaseFee := uint64(225 * utils.GWei)
	tests := []struct {
		name     string
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		timeMS   uint64
		want     *big.Int
	}{
		{
			name:     "subnetevm_activated",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				Extra:   (&subnetevm.Window{}).Bytes(),
				BaseFee: new(big.Int).SetUint64(testBaseFee),
			},
			timeMS: 1000,
			want: func() *big.Int {
				var (
					gasTarget                  = feeConfig.TargetGas.Uint64()
					gasUsed                    = uint64(0)
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = testBaseFee
					smoothingFactor            = feeConfig.BaseFeeChangeDenominator.Uint64()
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					delta                      = baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return new(big.Int).SetUint64(baseFee)
			}(),
		},
		{
			name:     "granite_activated_rounds_seconds",
			upgrades: extras.TestGraniteChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				Extra:   (&subnetevm.Window{}).Bytes(),
				BaseFee: new(big.Int).SetUint64(testBaseFee),
			},
			timeMS: 1999,
			want: func() *big.Int {
				var (
					gasTarget                  = feeConfig.TargetGas.Uint64()
					gasUsed                    = uint64(0)
					amountUnderTarget          = gasTarget - gasUsed
					parentBaseFee              = testBaseFee
					smoothingFactor            = feeConfig.BaseFeeChangeDenominator.Uint64()
					baseFeeFractionUnderTarget = amountUnderTarget * parentBaseFee / gasTarget
					delta                      = baseFeeFractionUnderTarget / smoothingFactor
					baseFee                    = parentBaseFee - delta
				)
				return new(big.Int).SetUint64(baseFee)
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &extras.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := EstimateNextBaseFee(config, feeConfig, test.parent, test.timeMS)
			require.NoError(err)
			require.Equal(test.want, got)
		})
	}
}
