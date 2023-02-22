// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ethereum/go-ethereum/common"
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

func TestVerifyFeeManagerConfig(t *testing.T) {
	admins := []common.Address{{1}}
	invalidFeeConfig := validFeeConfig
	invalidFeeConfig.GasLimit = big.NewInt(0)
	tests := []struct {
		name          string
		config        precompileconfig.Config
		ExpectedError string
	}{
		{
			name:          "invalid allow list config in fee manager allowlist",
			config:        NewConfig(big.NewInt(3), admins, admins, nil),
			ExpectedError: "cannot set address",
		},
		{
			name:          "invalid initial fee manager config",
			config:        NewConfig(big.NewInt(3), admins, nil, &invalidFeeConfig),
			ExpectedError: "gasLimit = 0 cannot be less than or equal to 0",
		},
		{
			name:          "nil initial fee manager config",
			config:        NewConfig(big.NewInt(3), admins, nil, &commontype.FeeConfig{}),
			ExpectedError: "gasLimit cannot be nil",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			err := tt.config.Verify()
			if tt.ExpectedError == "" {
				require.NoError(err)
			} else {
				require.ErrorContains(err, tt.ExpectedError)
			}
		})
	}
}

func TestEqualFeeManagerConfig(t *testing.T) {
	admins := []common.Address{{1}}
	enableds := []common.Address{{2}}
	tests := []struct {
		name     string
		config   precompileconfig.Config
		other    precompileconfig.Config
		expected bool
	}{
		{
			name:     "non-nil config and nil other",
			config:   NewConfig(big.NewInt(3), admins, enableds, nil),
			other:    nil,
			expected: false,
		},
		{
			name:     "different type",
			config:   NewConfig(big.NewInt(3), admins, enableds, nil),
			other:    precompileconfig.NewNoopStatefulPrecompileConfig(),
			expected: false,
		},
		{
			name:     "different timestamp",
			config:   NewConfig(big.NewInt(3), admins, nil, nil),
			other:    NewConfig(big.NewInt(4), admins, nil, nil),
			expected: false,
		},
		{
			name:     "different enabled",
			config:   NewConfig(big.NewInt(3), admins, nil, nil),
			other:    NewConfig(big.NewInt(3), admins, enableds, nil),
			expected: false,
		},
		{
			name:     "non-nil initial config and nil initial config",
			config:   NewConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			other:    NewConfig(big.NewInt(3), admins, nil, nil),
			expected: false,
		},
		{
			name:   "different initial config",
			config: NewConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			other: NewConfig(big.NewInt(3), admins, nil,
				func() *commontype.FeeConfig {
					c := validFeeConfig
					c.GasLimit = big.NewInt(123)
					return &c
				}()),
			expected: false,
		},
		{
			name:     "same config",
			config:   NewConfig(big.NewInt(3), admins, nil, &validFeeConfig),
			other:    NewConfig(big.NewInt(3), admins, nil, &validFeeConfig),
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
