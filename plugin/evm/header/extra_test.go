// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/acp176"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/utils"
	"github.com/stretchr/testify/require"
)

func TestExtraPrefix(t *testing.T) {
	tests := []struct {
		name                string
		upgrades            params.NetworkUpgrades
		parent              *types.Header
		header              *types.Header
		desiredTargetExcess *gas.Gas
		want                []byte
		wantErr             error
	}{
		{
			name:     "ap2",
			upgrades: params.TestApricotPhase2Config.NetworkUpgrades,
			header:   &types.Header{},
			want:     nil,
			wantErr:  nil,
		},
		{
			name: "ap3_first_block",
			upgrades: params.NetworkUpgrades{
				ApricotPhase3BlockTimestamp: utils.NewUint64(1),
			},
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header: &types.Header{
				Time: 1,
			},
			want: (&ap3.Window{}).Bytes(),
		},
		{
			name:     "ap3_genesis_block",
			upgrades: params.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{},
			want:   (&ap3.Window{}).Bytes(),
		},
		{
			name:     "ap3_invalid_fee_window",
			upgrades: params.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: ap3.ErrWindowInsufficientLength,
		},
		{
			name:     "ap3_invalid_timestamp",
			upgrades: params.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Time:   1,
				Extra:  (&ap3.Window{}).Bytes(),
			},
			header: &types.Header{
				Time: 0,
			},
			wantErr: errInvalidTimestamp,
		},
		{
			name:     "ap3_normal",
			upgrades: params.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: ap3.TargetGas,
				Extra: (&ap3.Window{
					1, 2, 3, 4,
				}).Bytes(),
			},
			header: &types.Header{
				Time: 1,
			},
			want: func() []byte {
				window := ap3.Window{
					1, 2, 3, 4,
				}
				window.Add(ap3.TargetGas, ap3.IntrinsicBlockGas)
				window.Shift(1)
				return window.Bytes()
			}(),
		},
		{
			name:     "ap4_genesis_block",
			upgrades: params.TestApricotPhase4Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{},
			want:   (&ap3.Window{}).Bytes(),
		},
		{
			name:     "ap4_no_block_gas_cost",
			upgrades: params.TestApricotPhase4Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: ap3.TargetGas,
				Extra:   (&ap3.Window{}).Bytes(),
			},
			header: &types.Header{
				Time: 2,
			},
			want: func() []byte {
				var window ap3.Window
				window.Add(ap3.TargetGas)
				window.Shift(2)
				return window.Bytes()
			}(),
		},
		{
			name:     "ap4_with_block_gas_cost",
			upgrades: params.TestApricotPhase4Config.NetworkUpgrades,
			parent: &types.Header{
				Number:       big.NewInt(1),
				GasUsed:      ap3.TargetGas,
				Extra:        (&ap3.Window{}).Bytes(),
				BlockGasCost: big.NewInt(ap4.MinBlockGasCost),
			},
			header: &types.Header{
				Time: 1,
			},
			want: func() []byte {
				var window ap3.Window
				window.Add(
					ap3.TargetGas,
					(ap4.TargetBlockRate-1)*ap4.BlockGasCostStep,
				)
				window.Shift(1)
				return window.Bytes()
			}(),
		},
		{
			name:     "ap4_with_extra_data_gas",
			upgrades: params.TestApricotPhase4Config.NetworkUpgrades,
			parent: &types.Header{
				Number:         big.NewInt(1),
				GasUsed:        ap3.TargetGas,
				Extra:          (&ap3.Window{}).Bytes(),
				ExtDataGasUsed: big.NewInt(5),
			},
			header: &types.Header{
				Time: 1,
			},
			want: func() []byte {
				var window ap3.Window
				window.Add(
					ap3.TargetGas,
					5,
				)
				window.Shift(1)
				return window.Bytes()
			}(),
		},
		{
			name:     "ap4_normal",
			upgrades: params.TestApricotPhase4Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: ap3.TargetGas,
				Extra: (&ap3.Window{
					1, 2, 3, 4,
				}).Bytes(),
				ExtDataGasUsed: big.NewInt(5),
				BlockGasCost:   big.NewInt(ap4.MinBlockGasCost),
			},
			header: &types.Header{
				Time: 1,
			},
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
				return window.Bytes()
			}(),
		},
		{
			name:     "ap5_no_extra_data_gas",
			upgrades: params.TestApricotPhase5Config.NetworkUpgrades,
			parent: &types.Header{
				Number:       big.NewInt(1),
				GasUsed:      ap5.TargetGas,
				Extra:        (&ap3.Window{}).Bytes(),
				BlockGasCost: big.NewInt(ap4.MinBlockGasCost),
			},
			header: &types.Header{
				Time: 1,
			},
			want: func() []byte {
				var window ap3.Window
				window.Add(ap5.TargetGas)
				window.Shift(1)
				return window.Bytes()
			}(),
		},
		{
			name:     "ap5_normal",
			upgrades: params.TestApricotPhase5Config.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: ap5.TargetGas,
				Extra: (&ap3.Window{
					1, 2, 3, 4,
				}).Bytes(),
				ExtDataGasUsed: big.NewInt(5),
				BlockGasCost:   big.NewInt(ap4.MinBlockGasCost),
			},
			header: &types.Header{
				Time: 1,
			},
			want: func() []byte {
				window := ap3.Window{
					1, 2, 3, 4,
				}
				window.Add(
					ap5.TargetGas,
					5,
				)
				window.Shift(1)
				return window.Bytes()
			}(),
		},
		{
			name: "fortuna_first_block",
			upgrades: params.NetworkUpgrades{
				FortunaTimestamp: utils.NewUint64(1),
			},
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header: &types.Header{
				Time:           1,
				GasUsed:        1,
				ExtDataGasUsed: big.NewInt(5),
			},
			want: (&acp176.State{
				Gas: gas.State{
					Capacity: acp176.MinMaxPerSecond - 6,
					Excess:   6,
				},
				TargetExcess: 0,
			}).Bytes(),
		},
		{
			name:     "fortuna_genesis_block",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				Time:           1,
				GasUsed:        2,
				ExtDataGasUsed: big.NewInt(1),
			},
			desiredTargetExcess: (*gas.Gas)(utils.NewUint64(3)),
			want: (&acp176.State{
				Gas: gas.State{
					Capacity: acp176.MinMaxPerSecond - 3,
					Excess:   3,
				},
				TargetExcess: 3,
			}).Bytes(),
		},
		{
			name:     "fortuna_invalid_fee_state",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_invalid_gas_used",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra:  (&acp176.State{}).Bytes(),
			},
			header: &types.Header{
				GasUsed: 1,
			},
			wantErr: gas.ErrInsufficientCapacity,
		},
		{
			name:     "fortuna_reduce_capacity",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Extra: (&acp176.State{
					Gas: gas.State{
						Capacity: 10_019_550, // [acp176.MinMaxCapacity] * e^(2*[acp176.MaxTargetExcessDiff] / [acp176.TargetConversion])
						Excess:   2_000_000_000 - 3,
					},
					TargetExcess: 2 * acp176.MaxTargetExcessDiff,
				}).Bytes(),
			},
			header: &types.Header{
				GasUsed:        2,
				ExtDataGasUsed: big.NewInt(1),
			},
			desiredTargetExcess: (*gas.Gas)(utils.NewUint64(0)),
			want: (&acp176.State{
				Gas: gas.State{
					Capacity: 10_009_770,    // [acp176.MinMaxCapacity] * e^([acp176.MaxTargetExcessDiff] / [acp176.TargetConversion])
					Excess:   1_998_047_816, // 2M * NewTarget / OldTarget
				},
				TargetExcess: acp176.MaxTargetExcessDiff,
			}).Bytes(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := ExtraPrefix(config, test.parent, test.header, test.desiredTargetExcess)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}

func TestVerifyExtraPrefix(t *testing.T) {
	tests := []struct {
		name     string
		upgrades params.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		wantErr  error
	}{
		{
			name:     "ap2",
			upgrades: params.TestApricotPhase2Config.NetworkUpgrades,
			header:   &types.Header{},
			wantErr:  nil,
		},
		{
			name:     "ap3_invalid_parent_header",
			upgrades: params.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: ap3.ErrWindowInsufficientLength,
		},
		{
			name:     "ap3_invalid_header",
			upgrades: params.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header:  &types.Header{},
			wantErr: errInvalidExtraPrefix,
		},
		{
			name:     "ap3_valid",
			upgrades: params.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				Extra: (&ap3.Window{}).Bytes(),
			},
			wantErr: nil,
		},
		{
			name:     "fortuna_invalid_header",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			header:   &types.Header{},
			wantErr:  acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_invalid_gas_consumed",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				GasUsed: 1,
				Extra:   (&acp176.State{}).Bytes(),
			},
			wantErr: gas.ErrInsufficientCapacity,
		},
		{
			name:     "fortuna_wrong_fee_state",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				Time:    1,
				GasUsed: 1,
				Extra: (&acp176.State{
					Gas: gas.State{
						Capacity: acp176.MinMaxPerSecond - 1,
						Excess:   1,
					},
					TargetExcess: acp176.MaxTargetExcessDiff + 1, // Too much of a diff
				}).Bytes(),
			},
			wantErr: errIncorrectFeeState,
		},
		{
			name:     "fortuna_valid",
			upgrades: params.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				Time:    1,
				GasUsed: 1,
				Extra: (&acp176.State{
					Gas: gas.State{
						Capacity: acp176.MinMaxPerSecond - 1,
						Excess:   1,
					},
					TargetExcess: acp176.MaxTargetExcessDiff,
				}).Bytes(),
			},
			wantErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			err := VerifyExtraPrefix(config, test.parent, test.header)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestVerifyExtra(t *testing.T) {
	tests := []struct {
		name     string
		rules    params.AvalancheRules
		extra    []byte
		expected error
	}{
		{
			name:     "initial_valid",
			rules:    params.AvalancheRules{},
			extra:    make([]byte, params.MaximumExtraDataSize),
			expected: nil,
		},
		{
			name:     "initial_invalid",
			rules:    params.AvalancheRules{},
			extra:    make([]byte, params.MaximumExtraDataSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "ap1_valid",
			rules: params.AvalancheRules{
				IsApricotPhase1: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "ap1_invalid",
			rules: params.AvalancheRules{
				IsApricotPhase1: true,
			},
			extra:    make([]byte, 1),
			expected: errInvalidExtraLength,
		},
		{
			name: "ap3_valid",
			rules: params.AvalancheRules{
				IsApricotPhase3: true,
			},
			extra:    make([]byte, ap3.WindowSize),
			expected: nil,
		},
		{
			name: "ap3_invalid_less",
			rules: params.AvalancheRules{
				IsApricotPhase3: true,
			},
			extra:    make([]byte, ap3.WindowSize-1),
			expected: errInvalidExtraLength,
		},
		{
			name: "ap3_invalid_more",
			rules: params.AvalancheRules{
				IsApricotPhase3: true,
			},
			extra:    make([]byte, ap3.WindowSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "durango_valid_min",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, ap3.WindowSize),
			expected: nil,
		},
		{
			name: "durango_valid_extra",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, ap3.WindowSize+1),
			expected: nil,
		},
		{
			name: "durango_invalid",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, ap3.WindowSize-1),
			expected: errInvalidExtraLength,
		},
		{
			name: "fortuna_valid_min",
			rules: params.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize),
			expected: nil,
		},
		{
			name: "fortuna_valid_extra",
			rules: params.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize+1),
			expected: nil,
		},
		{
			name: "fortuna_invalid",
			rules: params.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize-1),
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

func TestPredicateBytesFromExtra(t *testing.T) {
	tests := []struct {
		name     string
		rules    params.AvalancheRules
		extra    []byte
		expected []byte
	}{
		{
			name:     "empty_extra",
			extra:    nil,
			expected: nil,
		},
		{
			name:     "too_short",
			extra:    make([]byte, ap3.WindowSize-1),
			expected: nil,
		},
		{
			name:     "empty_predicate",
			extra:    make([]byte, ap3.WindowSize),
			expected: nil,
		},
		{
			name: "non_empty_predicate",
			extra: []byte{
				ap3.WindowSize: 5,
			},
			expected: []byte{5},
		},
		{
			name: "fortuna_empty_extra",
			rules: params.AvalancheRules{
				IsFortuna: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "fortuna_too_short",
			rules: params.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize-1),
			expected: nil,
		},
		{
			name: "fortuna_empty_predicate",
			rules: params.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize),
			expected: nil,
		},
		{
			name: "fortuna_non_empty_predicate",
			rules: params.AvalancheRules{
				IsFortuna: true,
			},
			extra: []byte{
				acp176.StateSize: 5,
			},
			expected: []byte{5},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := PredicateBytesFromExtra(test.rules, test.extra)
			require.Equal(t, test.expected, got)
		})
	}
}
