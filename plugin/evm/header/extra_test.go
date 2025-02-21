// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/utils"
	"github.com/stretchr/testify/require"
)

func TestExtraPrefix(t *testing.T) {
	tests := []struct {
		name      string
		upgrades  extras.NetworkUpgrades
		parent    *types.Header
		timestamp uint64
		want      []byte
		wantErr   error
	}{
		{
			name:     "ap2",
			upgrades: params.GetExtra(params.TestApricotPhase2Config).NetworkUpgrades,
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
			timestamp: 1,
			want:      feeWindowBytes(ap3.Window{}),
		},
		{
			name:     "ap3_genesis_block",
			upgrades: params.GetExtra(params.TestApricotPhase3Config).NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: feeWindowBytes(ap3.Window{}),
		},
		{
			name:     "ap3_invalid_fee_window",
			upgrades: params.GetExtra(params.TestApricotPhase3Config).NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			wantErr: errDynamicFeeWindowInsufficientLength,
		},
		{
			name:     "ap3_invalid_timestamp",
			upgrades: params.GetExtra(params.TestApricotPhase3Config).NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Time:   1,
				Extra:  feeWindowBytes(ap3.Window{}),
			},
			timestamp: 0,
			wantErr:   errInvalidTimestamp,
		},
		{
			name:     "ap3_normal",
			upgrades: params.GetExtra(params.TestApricotPhase3Config).NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: ap3.TargetGas,
				Extra: feeWindowBytes(ap3.Window{
					1, 2, 3, 4,
				}),
			},
			timestamp: 1,
			want: func() []byte {
				window := ap3.Window{
					1, 2, 3, 4,
				}
				window.Add(ap3.TargetGas, ap3.IntrinsicBlockGas)
				window.Shift(1)
				return feeWindowBytes(window)
			}(),
		},
		{
			name:     "ap4_genesis_block",
			upgrades: params.GetExtra(params.TestApricotPhase4Config).NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: feeWindowBytes(ap3.Window{}),
		},
		{
			name:     "ap4_no_block_gas_cost",
			upgrades: params.GetExtra(params.TestApricotPhase4Config).NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: ap3.TargetGas,
				Extra:   feeWindowBytes(ap3.Window{}),
			},
			timestamp: 2,
			want: func() []byte {
				var window ap3.Window
				window.Add(ap3.TargetGas)
				window.Shift(2)
				return feeWindowBytes(window)
			}(),
		},
		{
			name:     "ap4_with_block_gas_cost",
			upgrades: params.GetExtra(params.TestApricotPhase4Config).NetworkUpgrades,
			parent: &types.Header{
				Number:       big.NewInt(1),
				GasUsed:      ap3.TargetGas,
				Extra:        feeWindowBytes(ap3.Window{}),
				BlockGasCost: big.NewInt(ap4.MinBlockGasCost),
			},
			timestamp: 1,
			want: func() []byte {
				var window ap3.Window
				window.Add(
					ap3.TargetGas,
					(ap4.TargetBlockRate-1)*ap4.BlockGasCostStep,
				)
				window.Shift(1)
				return feeWindowBytes(window)
			}(),
		},
		{
			name:     "ap4_with_extra_data_gas",
			upgrades: params.GetExtra(params.TestApricotPhase4Config).NetworkUpgrades,
			parent: &types.Header{
				Number:         big.NewInt(1),
				GasUsed:        ap3.TargetGas,
				Extra:          feeWindowBytes(ap3.Window{}),
				ExtDataGasUsed: big.NewInt(5),
			},
			timestamp: 1,
			want: func() []byte {
				var window ap3.Window
				window.Add(
					ap3.TargetGas,
					5,
				)
				window.Shift(1)
				return feeWindowBytes(window)
			}(),
		},
		{
			name:     "ap4_normal",
			upgrades: params.GetExtra(params.TestApricotPhase4Config).NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: ap3.TargetGas,
				Extra: feeWindowBytes(ap3.Window{
					1, 2, 3, 4,
				}),
				ExtDataGasUsed: big.NewInt(5),
				BlockGasCost:   big.NewInt(ap4.MinBlockGasCost),
			},
			timestamp: 1,
			want: func() []byte {
				window := ap3.Window{
					1, 2, 3, 4,
				}
				window.Add(
					ap3.TargetGas,
					5,
					(ap4.TargetBlockRate-1)*ap4.BlockGasCostStep,
				)
				window.Shift(1)
				return feeWindowBytes(window)
			}(),
		},
		{
			name:     "ap5_no_extra_data_gas",
			upgrades: params.GetExtra(params.TestApricotPhase5Config).NetworkUpgrades,
			parent: &types.Header{
				Number:       big.NewInt(1),
				GasUsed:      ap5.TargetGas,
				Extra:        feeWindowBytes(ap3.Window{}),
				BlockGasCost: big.NewInt(ap4.MinBlockGasCost),
			},
			timestamp: 1,
			want: func() []byte {
				var window ap3.Window
				window.Add(ap5.TargetGas)
				window.Shift(1)
				return feeWindowBytes(window)
			}(),
		},
		{
			name:     "ap5_normal",
			upgrades: params.GetExtra(params.TestApricotPhase5Config).NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: ap5.TargetGas,
				Extra: feeWindowBytes(ap3.Window{
					1, 2, 3, 4,
				}),
				ExtDataGasUsed: big.NewInt(5),
				BlockGasCost:   big.NewInt(ap4.MinBlockGasCost),
			},
			timestamp: 1,
			want: func() []byte {
				window := ap3.Window{
					1, 2, 3, 4,
				}
				window.Add(
					ap5.TargetGas,
					5,
				)
				window.Shift(1)
				return feeWindowBytes(window)
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &extras.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := ExtraPrefix(config, test.parent, test.timestamp)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}

func TestVerifyExtra(t *testing.T) {
	tests := []struct {
		name     string
		rules    extras.AvalancheRules
		extra    []byte
		expected error
	}{
		{
			name:     "initial_valid",
			rules:    extras.AvalancheRules{},
			extra:    make([]byte, params.MaximumExtraDataSize),
			expected: nil,
		},
		{
			name:     "initial_invalid",
			rules:    extras.AvalancheRules{},
			extra:    make([]byte, params.MaximumExtraDataSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "ap1_valid",
			rules: extras.AvalancheRules{
				IsApricotPhase1: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "ap1_invalid",
			rules: extras.AvalancheRules{
				IsApricotPhase1: true,
			},
			extra:    make([]byte, 1),
			expected: errInvalidExtraLength,
		},
		{
			name: "ap3_valid",
			rules: extras.AvalancheRules{
				IsApricotPhase3: true,
			},
			extra:    make([]byte, FeeWindowSize),
			expected: nil,
		},
		{
			name: "ap3_invalid_less",
			rules: extras.AvalancheRules{
				IsApricotPhase3: true,
			},
			extra:    make([]byte, FeeWindowSize-1),
			expected: errInvalidExtraLength,
		},
		{
			name: "ap3_invalid_more",
			rules: extras.AvalancheRules{
				IsApricotPhase3: true,
			},
			extra:    make([]byte, FeeWindowSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "durango_valid_min",
			rules: extras.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, FeeWindowSize),
			expected: nil,
		},
		{
			name: "durango_valid_extra",
			rules: extras.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, FeeWindowSize+1),
			expected: nil,
		},
		{
			name: "durango_invalid",
			rules: extras.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, FeeWindowSize-1),
			expected: errInvalidExtraLength,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyExtra(test.rules, test.extra)
			require.ErrorIs(t, err, test.expected)
		})
	}
}
