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
		name      string
		upgrades  params.NetworkUpgrades
		parent    *types.Header
		timestamp uint64
		want      []byte
		wantErr   error
	}{
		{
			name:     "pre_subnet_evm",
			upgrades: params.TestPreSubnetEVMChainConfig.NetworkUpgrades,
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
			timestamp: 1,
			want:      feeWindowBytes(subnetevm.Window{}),
		},
		{
			name:     "subnet_evm_genesis_block",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			want: feeWindowBytes(subnetevm.Window{}),
		},
		{
			name:     "subnet_evm_invalid_fee_window",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			wantErr: errDynamicFeeWindowInsufficientLength,
		},
		{
			name:     "subnet_evm_invalid_timestamp",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
				Time:   1,
				Extra:  feeWindowBytes(subnetevm.Window{}),
			},
			timestamp: 0,
			wantErr:   errInvalidTimestamp,
		},
		{
			name:     "subnet_evm_normal",
			upgrades: params.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: targetGas,
				Extra: feeWindowBytes(subnetevm.Window{
					1, 2, 3, 4,
				}),
				BlockGasCost: big.NewInt(blockGas),
			},
			timestamp: 1,
			want: func() []byte {
				window := subnetevm.Window{
					1, 2, 3, 4,
				}
				window.Add(targetGas)
				window.Shift(1)
				return feeWindowBytes(window)
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &params.ChainConfig{
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
			extra:    make([]byte, FeeWindowSize),
			expected: nil,
		},
		{
			name: "subnet_evm_invalid_less",
			rules: params.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, FeeWindowSize-1),
			expected: errInvalidExtraLength,
		},
		{
			name: "subnet_evm_invalid_more",
			rules: params.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, FeeWindowSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "durango_valid_min",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, FeeWindowSize),
			expected: nil,
		},
		{
			name: "durango_valid_extra",
			rules: params.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, FeeWindowSize+1),
			expected: nil,
		},
		{
			name: "durango_invalid",
			rules: params.AvalancheRules{
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

func TestPredicateBytesFromExtra(t *testing.T) {
	tests := []struct {
		name     string
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
			extra:    make([]byte, FeeWindowSize-1),
			expected: nil,
		},
		{
			name:     "empty_predicate",
			extra:    make([]byte, FeeWindowSize),
			expected: nil,
		},
		{
			name: "non_empty_predicate",
			extra: []byte{
				FeeWindowSize: 5,
			},
			expected: []byte{5},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := PredicateBytesFromExtra(test.extra)
			require.Equal(t, test.expected, got)
		})
	}
}
