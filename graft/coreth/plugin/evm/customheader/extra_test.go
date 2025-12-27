// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	os.Exit(m.Run())
}

func TestExtraPrefix(t *testing.T) {
	tests := []struct {
		name                string
		upgrades            extras.NetworkUpgrades
		parent              *types.Header
		header              *types.Header
		desiredTargetExcess *gas.Gas
		want                []byte
		wantErr             error
	}{
		{
			name:     "ap2",
			upgrades: extras.TestApricotPhase2Config.NetworkUpgrades,
			header:   &types.Header{},
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
			header: &types.Header{
				Time: 1,
			},
			want: (&ap3.Window{}).Bytes(),
		},
		{
			name:     "ap3_genesis_block",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{},
			want:   (&ap3.Window{}).Bytes(),
		},
		{
			name:     "ap3_invalid_fee_window",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
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
			header: &types.Header{
				Time: 0,
			},
			wantErr: errInvalidTimestamp,
		},
		{
			name:     "ap3_normal",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
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
			upgrades: extras.TestApricotPhase4Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{},
			want:   (&ap3.Window{}).Bytes(),
		},
		{
			name:     "ap4_no_block_gas_cost",
			upgrades: extras.TestApricotPhase4Config.NetworkUpgrades,
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
			upgrades: extras.TestApricotPhase4Config.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(
				&types.Header{
					Number:  big.NewInt(1),
					GasUsed: ap3.TargetGas,
					Extra:   (&ap3.Window{}).Bytes(),
				},
				&customtypes.HeaderExtra{
					BlockGasCost: big.NewInt(ap4.MinBlockGasCost),
				},
			),
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
			upgrades: extras.TestApricotPhase4Config.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(
				&types.Header{
					Number:  big.NewInt(1),
					GasUsed: ap3.TargetGas,
					Extra:   (&ap3.Window{}).Bytes(),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(5),
				},
			),
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
			upgrades: extras.TestApricotPhase4Config.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(
				&types.Header{
					Number:  big.NewInt(1),
					GasUsed: ap3.TargetGas,
					Extra: (&ap3.Window{
						1, 2, 3, 4,
					}).Bytes(),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(5),
					BlockGasCost:   big.NewInt(ap4.MinBlockGasCost),
				},
			),
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
			upgrades: extras.TestApricotPhase5Config.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(
				&types.Header{
					Number:  big.NewInt(1),
					GasUsed: ap5.TargetGas,
					Extra:   (&ap3.Window{}).Bytes(),
				},
				&customtypes.HeaderExtra{
					BlockGasCost: big.NewInt(ap4.MinBlockGasCost),
				},
			),
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
			upgrades: extras.TestApricotPhase5Config.NetworkUpgrades,
			parent: customtypes.WithHeaderExtra(
				&types.Header{
					Number:  big.NewInt(1),
					GasUsed: ap5.TargetGas,
					Extra: (&ap3.Window{
						1, 2, 3, 4,
					}).Bytes(),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(5),
					BlockGasCost:   big.NewInt(ap4.MinBlockGasCost),
				},
			),
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
			upgrades: extras.NetworkUpgrades{
				FortunaTimestamp: utils.NewUint64(1),
			},
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header: customtypes.WithHeaderExtra(
				&types.Header{
					Time:    1,
					GasUsed: 1,
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(5),
				},
			),
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
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: customtypes.WithHeaderExtra(
				&types.Header{
					Time:    1,
					GasUsed: 2,
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(1),
				},
			),
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
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_invalid_gas_used",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
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
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
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
			header: customtypes.WithHeaderExtra(
				&types.Header{
					GasUsed: 2,
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(1),
				},
			),
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

			config := &extras.ChainConfig{
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
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		wantErr  error
	}{
		{
			name:     "ap2",
			upgrades: extras.TestApricotPhase2Config.NetworkUpgrades,
			header:   &types.Header{},
			wantErr:  nil,
		},
		{
			name:     "ap3_invalid_parent_header",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: ap3.ErrWindowInsufficientLength,
		},
		{
			name:     "ap3_invalid_header",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header:  &types.Header{},
			wantErr: errInvalidExtraPrefix,
		},
		{
			name:     "ap3_valid",
			upgrades: extras.TestApricotPhase3Config.NetworkUpgrades,
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
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
			header:   &types.Header{},
			wantErr:  acp176.ErrStateInsufficientLength,
		},
		{
			name:     "fortuna_invalid_gas_consumed",
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
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
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
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
			upgrades: extras.TestFortunaChainConfig.NetworkUpgrades,
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
			config := &extras.ChainConfig{
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
		rules    extras.AvalancheRules
		extra    []byte
		expected error
	}{
		{
			name:     "initial_valid",
			rules:    extras.AvalancheRules{},
			extra:    make([]byte, ap0.MaximumExtraDataSize),
			expected: nil,
		},
		{
			name:     "initial_invalid",
			rules:    extras.AvalancheRules{},
			extra:    make([]byte, ap0.MaximumExtraDataSize+1),
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
			extra:    make([]byte, ap3.WindowSize),
			expected: nil,
		},
		{
			name: "ap3_invalid_less",
			rules: extras.AvalancheRules{
				IsApricotPhase3: true,
			},
			extra:    make([]byte, ap3.WindowSize-1),
			expected: errInvalidExtraLength,
		},
		{
			name: "ap3_invalid_more",
			rules: extras.AvalancheRules{
				IsApricotPhase3: true,
			},
			extra:    make([]byte, ap3.WindowSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "durango_valid_min",
			rules: extras.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, ap3.WindowSize),
			expected: nil,
		},
		{
			name: "durango_valid_extra",
			rules: extras.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, ap3.WindowSize+1),
			expected: nil,
		},
		{
			name: "durango_invalid",
			rules: extras.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, ap3.WindowSize-1),
			expected: errInvalidExtraLength,
		},
		{
			name: "fortuna_valid_min",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize),
			expected: nil,
		},
		{
			name: "fortuna_valid_extra",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize+1),
			expected: nil,
		},
		{
			name: "fortuna_invalid",
			rules: extras.AvalancheRules{
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
		rules    extras.AvalancheRules
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
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "fortuna_too_short",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize-1),
			expected: nil,
		},
		{
			name: "fortuna_empty_predicate",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize),
			expected: nil,
		},
		{
			name: "fortuna_non_empty_predicate",
			rules: extras.AvalancheRules{
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

func TestSetPredicateBytesInExtra(t *testing.T) {
	tests := []struct {
		name      string
		rules     extras.AvalancheRules
		extra     []byte
		predicate []byte
		want      []byte
	}{
		{
			name: "empty_extra_predicate",
			want: make([]byte, ap3.WindowSize),
		},
		{
			name: "empty_extra_predicate_fortuna",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			want: make([]byte, acp176.StateSize),
		},
		{
			name:      "extra_too_short",
			extra:     []byte{1},
			predicate: []byte{2},
			want: []byte{
				0:              1,
				ap3.WindowSize: 2,
			},
		},
		{
			name: "extra_too_short_fortuna",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:     []byte{1},
			predicate: []byte{2},
			want: []byte{
				0:                1,
				acp176.StateSize: 2,
			},
		},
		{
			name: "extra_too_long",
			extra: []byte{
				ap3.WindowSize: 1,
			},
			predicate: []byte{2},
			want: []byte{
				ap3.WindowSize: 2,
			},
		},
		{
			name: "extra_too_long_fortuna",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra: []byte{
				acp176.StateSize: 1,
			},
			predicate: []byte{2},
			want: []byte{
				acp176.StateSize: 2,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := SetPredicateBytesInExtra(test.rules, test.extra, test.predicate)
			require.Equal(t, test.want, got)
		})
	}
}

func TestPredicateBytesExtra(t *testing.T) {
	tests := []struct {
		name                   string
		rules                  extras.AvalancheRules
		extra                  []byte
		predicate              []byte
		wantExtraWithPredicate []byte
	}{
		{
			name:                   "empty_extra_predicate",
			extra:                  nil,
			predicate:              nil,
			wantExtraWithPredicate: make([]byte, ap3.WindowSize),
		},
		{
			name: "empty_extra_predicate_fortuna",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:                  nil,
			predicate:              nil,
			wantExtraWithPredicate: make([]byte, acp176.StateSize),
		},
		{
			name: "extra_too_short",
			extra: []byte{
				0:                  1,
				ap3.WindowSize - 1: 0,
			},
			predicate: []byte{2},
			wantExtraWithPredicate: []byte{
				0:              1,
				ap3.WindowSize: 2,
			},
		},
		{
			name: "extra_too_short_fortuna",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra: []byte{
				0:                    1,
				acp176.StateSize - 1: 0,
			},
			predicate: []byte{2},
			wantExtraWithPredicate: []byte{
				0:                1,
				acp176.StateSize: 2,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var wantPredicate []byte
			if test.predicate != nil {
				wantPredicate = make([]byte, len(test.predicate))
				copy(wantPredicate, test.predicate)
			}

			gotExtra := SetPredicateBytesInExtra(test.rules, test.extra, test.predicate)
			require.Equal(t, test.wantExtraWithPredicate, gotExtra)
			gotPredicate := PredicateBytesFromExtra(test.rules, gotExtra)
			require.Equal(t, wantPredicate, gotPredicate)
		})
	}
}
