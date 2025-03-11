// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm/upgrade/subnetevm"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/stretchr/testify/require"
)

const (
	targetGas = 10_000_000
	blockGas  = 1_000_000
)

func TestExtraPrefix(t *testing.T) {
	tests := []struct {
		name     string
		upgrades params.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		want     []byte
		wantErr  error
	}{
		{
			name:     "pre_subnet_evm",
			upgrades: params.TestPreSubnetEVMChainConfig.NetworkUpgrades,
			header:   &types.Header{},
			want:     nil,
			wantErr:  nil,
		},
		{
			name: "subnet_evm_first_block",
			upgrades: params.NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
			},
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header: &types.Header{
				Time: 1,
			},
			want: (&subnetevm.Window{}).Bytes(),
		},
		{
			name:     "subnet_evm_genesis_block",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{},
			want:   (&subnetevm.Window{}).Bytes(),
		},
		{
			name:     "subnet_evm_invalid_fee_window",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: subnetevm.ErrWindowInsufficientLength,
		},
		{
			name:     "subnet_evm_invalid_timestamp",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Time:   1,
				Extra:  (&subnetevm.Window{}).Bytes(),
			},
			header: &types.Header{
				Time: 0,
			},
			wantErr: errInvalidTimestamp,
		},
		{
			name:     "subnet_evm_normal",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: targetGas,
				Extra: (&subnetevm.Window{
					1, 2, 3, 4,
				}).Bytes(),
				BlockGasCost: big.NewInt(blockGas),
			},
			header: &types.Header{
				Time: 1,
			},
			want: func() []byte {
				window := subnetevm.Window{
					1, 2, 3, 4,
				}
				window.Add(targetGas)
				window.Shift(1)
				return window.Bytes()
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &params.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			got, err := ExtraPrefix(config, test.parent, test.header)
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
			name:     "pre_subnet_evm",
			upgrades: params.TestPreSubnetEVMChainConfig.NetworkUpgrades,
			header:   &types.Header{},
			wantErr:  nil,
		},
		{
			name:     "subnet_evm_invalid_parent_header",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: subnetevm.ErrWindowInsufficientLength,
		},
		{
			name:     "subnet_evm_invalid_header",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header:  &types.Header{},
			wantErr: errInvalidExtraPrefix,
		},
		{
			name:     "subnet_evm_valid",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{
				Extra: (&subnetevm.Window{}).Bytes(),
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
			name: "subnet_evm_valid",
			rules: params.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, subnetevm.WindowSize),
			expected: nil,
		},
		{
			name: "subnet_evm_invalid_less",
			rules: params.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, subnetevm.WindowSize-1),
			expected: errInvalidExtraLength,
		},
		{
			name: "subnet_evm_invalid_more",
			rules: params.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, subnetevm.WindowSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "durango_valid_min",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, subnetevm.WindowSize),
			expected: nil,
		},
		{
			name: "durango_valid_extra",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, subnetevm.WindowSize+1),
			expected: nil,
		},
		{
			name: "durango_invalid",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, subnetevm.WindowSize-1),
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
			extra:    make([]byte, subnetevm.WindowSize-1),
			expected: nil,
		},
		{
			name:     "empty_predicate",
			extra:    make([]byte, subnetevm.WindowSize),
			expected: nil,
		},
		{
			name: "non_empty_predicate",
			extra: []byte{
				subnetevm.WindowSize: 5,
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
