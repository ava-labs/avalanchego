// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params/extras"
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
		name        string
		upgrades    extras.NetworkUpgrades
		parent      *types.Header
		parentExtra *types.HeaderExtra
		header      *types.Header
		want        []byte
		wantErr     error
	}{
		{
			name:     "pre_subnet_evm",
			upgrades: extras.TestPreSubnetEVMChainConfig.NetworkUpgrades,
			header:   &types.Header{},
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
			header: &types.Header{
				Time: 1,
			},
			want: (&subnetevm.Window{}).Bytes(),
		},
		{
			name:     "subnet_evm_genesis_block",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header: &types.Header{},
			want:   (&subnetevm.Window{}).Bytes(),
		},
		{
			name:     "subnet_evm_invalid_fee_window",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
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
			header: &types.Header{
				Time: 0,
			},
			wantErr: errInvalidTimestamp,
		},
		{
			name:     "subnet_evm_normal",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number:  big.NewInt(1),
				GasUsed: targetGas,
				Extra: (&subnetevm.Window{
					1, 2, 3, 4,
				}).Bytes(),
			},
			parentExtra: &types.HeaderExtra{
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

			config := &extras.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			if test.parentExtra != nil {
				types.SetHeaderExtra(test.parent, test.parentExtra)
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
		upgrades extras.NetworkUpgrades
		parent   *types.Header
		header   *types.Header
		wantErr  error
	}{
		{
			name:     "pre_subnet_evm",
			upgrades: extras.TestPreSubnetEVMChainConfig.NetworkUpgrades,
			header:   &types.Header{},
			wantErr:  nil,
		},
		{
			name:     "subnet_evm_invalid_parent_header",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(1),
			},
			header:  &types.Header{},
			wantErr: subnetevm.ErrWindowInsufficientLength,
		},
		{
			name:     "subnet_evm_invalid_header",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
			parent: &types.Header{
				Number: big.NewInt(0),
			},
			header:  &types.Header{},
			wantErr: errInvalidExtraPrefix,
		},
		{
			name:     "subnet_evm_valid",
			upgrades: extras.TestSubnetEVMChainConfig.NetworkUpgrades,
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
			extra:    make([]byte, extras.MaximumExtraDataSize),
			expected: nil,
		},
		{
			name:     "initial_invalid",
			rules:    extras.AvalancheRules{},
			extra:    make([]byte, extras.MaximumExtraDataSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "subnet_evm_valid",
			rules: extras.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, subnetevm.WindowSize),
			expected: nil,
		},
		{
			name: "subnet_evm_invalid_less",
			rules: extras.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, subnetevm.WindowSize-1),
			expected: errInvalidExtraLength,
		},
		{
			name: "subnet_evm_invalid_more",
			rules: extras.AvalancheRules{
				IsSubnetEVM: true,
			},
			extra:    make([]byte, subnetevm.WindowSize+1),
			expected: errInvalidExtraLength,
		},
		{
			name: "durango_valid_min",
			rules: extras.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, subnetevm.WindowSize),
			expected: nil,
		},
		{
			name: "durango_valid_extra",
			rules: extras.AvalancheRules{
				IsDurango: true,
			},
			extra:    make([]byte, subnetevm.WindowSize+1),
			expected: nil,
		},
		{
			name: "durango_invalid",
			rules: extras.AvalancheRules{
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

func TestPredicateBytesExtra(t *testing.T) {
	tests := []struct {
		name                   string
		extra                  []byte
		predicate              []byte
		wantExtraWithPredicate []byte
		wantPredicateBytes     []byte
	}{
		{
			name:                   "empty_extra_predicate",
			extra:                  nil,
			predicate:              nil,
			wantExtraWithPredicate: make([]byte, subnetevm.WindowSize),
			wantPredicateBytes:     nil,
		},
		{
			name: "extra_too_short",
			extra: []byte{
				0:                        1,
				subnetevm.WindowSize - 1: 0,
			},
			predicate: []byte{2},
			wantExtraWithPredicate: []byte{
				0:                    1,
				subnetevm.WindowSize: 2,
			},
			wantPredicateBytes: []byte{2},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotExtra := SetPredicateBytesInExtra(test.extra, test.predicate)
			require.Equal(t, test.wantExtraWithPredicate, gotExtra)
			gotPredicateBytes := PredicateBytesFromExtra(gotExtra)
			require.Equal(t, test.wantPredicateBytes, gotPredicateBytes)
		})
	}
}
