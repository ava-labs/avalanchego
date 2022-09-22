// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/require"
)

var validFeeConfig = commontype.FeeConfig{
	GasLimit:        big.NewInt(8_000_000),
	TargetBlockRate: 2, // in seconds

	MinBaseFee:               big.NewInt(25_000_000_000),
	TargetGas:                big.NewInt(15_000_000),
	BaseFeeChangeDenominator: big.NewInt(36),

	MinBlockGasCost:  big.NewInt(0),
	MaxBlockGasCost:  big.NewInt(1_000_000),
	BlockGasCostStep: big.NewInt(200_000),
}

func TestVerifyPrecompileUpgrades(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name          string
		config        StatefulPrecompileConfig
		expectedError string
	}{
		{
			name:          "invalid allow list config in tx allowlist",
			config:        NewTxAllowListConfig(big.NewInt(3), admins, admins),
			expectedError: "cannot set address",
		},
		{
			name:          "nil member allow list config in tx allowlist",
			config:        NewTxAllowListConfig(big.NewInt(3), nil, nil),
			expectedError: "",
		},
		{
			name:          "empty member allow list config in tx allowlist",
			config:        NewTxAllowListConfig(big.NewInt(3), []common.Address{}, []common.Address{}),
			expectedError: "",
		},
		{
			name:          "valid allow list config in tx allowlist",
			config:        NewTxAllowListConfig(big.NewInt(3), admins, enableds),
			expectedError: "",
		},
		{
			name:          "invalid allow list config in deployer allowlist",
			config:        NewContractDeployerAllowListConfig(big.NewInt(3), admins, admins),
			expectedError: "cannot set address",
		},
		{
			name:          "invalid allow list config in native minter allowlist",
			config:        NewContractNativeMinterConfig(big.NewInt(3), admins, admins, nil),
			expectedError: "cannot set address",
		},
		{
			name:          "invalid allow list config in fee manager allowlist",
			config:        NewFeeManagerConfig(big.NewInt(3), admins, admins, nil),
			expectedError: "cannot set address",
		},
		{
			name: "invalid initial fee manager config",
			config: NewFeeManagerConfig(big.NewInt(3), admins, nil,
				&commontype.FeeConfig{
					GasLimit: big.NewInt(0),
				}),
			expectedError: "gasLimit = 0 cannot be less than or equal to 0",
		},
		{
			name: "nil amount in native minter config",
			config: NewContractNativeMinterConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(123),
					common.HexToAddress("0x02"): nil,
				}),
			expectedError: "initial mint cannot contain nil",
		},
		{
			name: "negative amount in native minter config",
			config: NewContractNativeMinterConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(123),
					common.HexToAddress("0x02"): math.NewHexOrDecimal256(-1),
				}),
			expectedError: "initial mint cannot contain invalid amount",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			err := tt.config.Verify()
			if tt.expectedError == "" {
				require.NoError(err)
			} else {
				require.ErrorContains(err, tt.expectedError)
			}
		})
	}
}

func TestEqualTxAllowListConfig(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name     string
		config   StatefulPrecompileConfig
		other    StatefulPrecompileConfig
		expected bool
	}{
		{
			name:     "non-nil config and nil other",
			config:   NewTxAllowListConfig(big.NewInt(3), admins, enableds),
			other:    nil,
			expected: false,
		},
		{
			name:     "different type",
			config:   NewTxAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewContractDeployerAllowListConfig(big.NewInt(3), admins, enableds),
			expected: false,
		},
		{
			name:     "different admin",
			config:   NewTxAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewTxAllowListConfig(big.NewInt(3), []common.Address{{3}}, enableds),
			expected: false,
		},
		{
			name:     "different enabled",
			config:   NewTxAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewTxAllowListConfig(big.NewInt(3), admins, []common.Address{{3}}),
			expected: false,
		},
		{
			name:     "different version",
			config:   NewTxAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewTxAllowListConfig(big.NewInt(4), admins, enableds),
			expected: false,
		},
		{
			name:     "same config",
			config:   NewTxAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewTxAllowListConfig(big.NewInt(3), admins, enableds),
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.config.Equal(tt.other))
		})
	}
}

func TestEqualContractDeployerAllowListConfig(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name     string
		config   StatefulPrecompileConfig
		other    StatefulPrecompileConfig
		expected bool
	}{
		{
			name:     "non-nil config and nil other",
			config:   NewContractDeployerAllowListConfig(big.NewInt(3), admins, enableds),
			other:    nil,
			expected: false,
		},
		{
			name:     "different type",
			config:   NewContractDeployerAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewTxAllowListConfig(big.NewInt(3), admins, enableds),
			expected: false,
		},
		{
			name:     "different admin",
			config:   NewContractDeployerAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewContractDeployerAllowListConfig(big.NewInt(3), []common.Address{{3}}, enableds),
			expected: false,
		},
		{
			name:     "different enabled",
			config:   NewContractDeployerAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewContractDeployerAllowListConfig(big.NewInt(3), admins, []common.Address{{3}}),
			expected: false,
		},
		{
			name:     "different version",
			config:   NewContractDeployerAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewContractDeployerAllowListConfig(big.NewInt(4), admins, enableds),
			expected: false,
		},
		{
			name:     "same config",
			config:   NewContractDeployerAllowListConfig(big.NewInt(3), admins, enableds),
			other:    NewContractDeployerAllowListConfig(big.NewInt(3), admins, enableds),
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.config.Equal(tt.other))
		})
	}
}

func TestEqualContractNativeMinterConfig(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name     string
		config   StatefulPrecompileConfig
		other    StatefulPrecompileConfig
		expected bool
	}{
		{
			name:     "non-nil config and nil other",
			config:   NewContractNativeMinterConfig(big.NewInt(3), admins, enableds, nil),
			other:    nil,
			expected: false,
		},
		{
			name:     "different type",
			config:   NewContractNativeMinterConfig(big.NewInt(3), admins, enableds, nil),
			other:    NewTxAllowListConfig(big.NewInt(3), []common.Address{{1}}, []common.Address{{2}}),
			expected: false,
		},
		{
			name:     "different version",
			config:   NewContractNativeMinterConfig(big.NewInt(3), admins, nil, nil),
			other:    NewContractNativeMinterConfig(big.NewInt(4), admins, nil, nil),
			expected: false,
		},
		{
			name:     "different enabled",
			config:   NewContractNativeMinterConfig(big.NewInt(3), admins, nil, nil),
			other:    NewContractNativeMinterConfig(big.NewInt(3), admins, enableds, nil),
			expected: false,
		},
		{
			name: "different initial mint amounts",
			config: NewContractNativeMinterConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			other: NewContractNativeMinterConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(2),
				}),
			expected: false,
		},
		{
			name: "different initial mint addresses",
			config: NewContractNativeMinterConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			other: NewContractNativeMinterConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x02"): math.NewHexOrDecimal256(1),
				}),
			expected: false,
		},

		{
			name: "same config",
			config: NewContractNativeMinterConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			other: NewContractNativeMinterConfig(big.NewInt(3), admins, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.config.Equal(tt.other))
		})
	}
}

func TestEqualFeeConfigManagerConfig(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name     string
		config   StatefulPrecompileConfig
		other    StatefulPrecompileConfig
		expected bool
	}{
		{
			name:     "non-nil config and nil other",
			config:   NewFeeManagerConfig(big.NewInt(3), admins, enableds, nil),
			other:    nil,
			expected: false,
		},
		{
			name:     "different type",
			config:   NewFeeManagerConfig(big.NewInt(3), admins, enableds, nil),
			other:    NewTxAllowListConfig(big.NewInt(3), []common.Address{{1}}, []common.Address{{2}}),
			expected: false,
		},
		{
			name:     "different version",
			config:   NewFeeManagerConfig(big.NewInt(3), admins, nil, nil),
			other:    NewFeeManagerConfig(big.NewInt(4), admins, nil, nil),
			expected: false,
		},
		{
			name:     "different enabled",
			config:   NewFeeManagerConfig(big.NewInt(3), admins, nil, nil),
			other:    NewFeeManagerConfig(big.NewInt(3), admins, enableds, nil),
			expected: false,
		},
		{
			name:     "non-nil initial config and nil initial config",
			config:   NewFeeManagerConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			other:    NewFeeManagerConfig(big.NewInt(3), admins, nil, nil),
			expected: false,
		},
		{
			name:   "different initial config",
			config: NewFeeManagerConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			other: NewFeeManagerConfig(big.NewInt(3), admins, nil,
				func() *commontype.FeeConfig {
					c := validFeeConfig
					c.GasLimit = big.NewInt(123)
					return &c
				}()),
			expected: false,
		},
		{
			name:     "same config",
			config:   NewFeeManagerConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			other:    NewFeeManagerConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.config.Equal(tt.other))
		})
	}
}
