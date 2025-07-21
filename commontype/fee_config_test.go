// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontype

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	tests := []struct {
		name          string
		config        *FeeConfig
		expectedError string
	}{
		{
			name: "nil gasLimit in FeeConfig",
			config: &FeeConfig{
				// GasLimit:        big.NewInt(8_000_000)
				TargetBlockRate: 2, // in seconds

				MinBaseFee:               big.NewInt(25_000_000_000),
				TargetGas:                big.NewInt(15_000_000),
				BaseFeeChangeDenominator: big.NewInt(36),

				MinBlockGasCost:  big.NewInt(0),
				MaxBlockGasCost:  big.NewInt(1_000_000),
				BlockGasCostStep: big.NewInt(200_000),
			},
			expectedError: "gasLimit cannot be nil",
		},
		{
			name:          "invalid GasLimit in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.GasLimit = big.NewInt(0); return &c }(),
			expectedError: "gasLimit = 0 cannot be less than or equal to 0",
		},
		{
			name:          "invalid TargetBlockRate in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.TargetBlockRate = 0; return &c }(),
			expectedError: "targetBlockRate = 0 cannot be less than or equal to 0",
		},
		{
			name:          "invalid MinBaseFee in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.MinBaseFee = big.NewInt(-1); return &c }(),
			expectedError: "minBaseFee = -1 cannot be less than 0",
		},
		{
			name:          "invalid TargetGas in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.TargetGas = big.NewInt(0); return &c }(),
			expectedError: "targetGas = 0 cannot be less than or equal to 0",
		},
		{
			name:          "invalid BaseFeeChangeDenominator in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.BaseFeeChangeDenominator = big.NewInt(0); return &c }(),
			expectedError: "baseFeeChangeDenominator = 0 cannot be less than or equal to 0",
		},
		{
			name:          "invalid MinBlockGasCost in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.MinBlockGasCost = big.NewInt(-1); return &c }(),
			expectedError: "minBlockGasCost = -1 cannot be less than 0",
		},
		{
			name:          "valid FeeConfig",
			config:        &ValidTestFeeConfig,
			expectedError: "",
		},
		{
			name: "MinBlockGasCost bigger than MaxBlockGasCost in FeeConfig",
			config: func() *FeeConfig {
				c := ValidTestFeeConfig
				c.MinBlockGasCost = big.NewInt(2)
				c.MaxBlockGasCost = big.NewInt(1)
				return &c
			}(),
			expectedError: "minBlockGasCost = 2 cannot be greater than maxBlockGasCost = 1",
		},
		{
			name:          "invalid BlockGasCostStep in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.BlockGasCostStep = big.NewInt(-1); return &c }(),
			expectedError: "blockGasCostStep = -1 cannot be less than 0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Verify()
			if test.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.expectedError)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        *FeeConfig
		b        *FeeConfig
		expected bool
	}{
		{
			name: "equal",
			a:    &ValidTestFeeConfig,
			b: &FeeConfig{
				GasLimit:        big.NewInt(8_000_000),
				TargetBlockRate: 2, // in seconds

				MinBaseFee:               big.NewInt(25_000_000_000),
				TargetGas:                big.NewInt(15_000_000),
				BaseFeeChangeDenominator: big.NewInt(36),

				MinBlockGasCost:  big.NewInt(0),
				MaxBlockGasCost:  big.NewInt(1_000_000),
				BlockGasCostStep: big.NewInt(200_000),
			},
			expected: true,
		},
		{
			name:     "not equal",
			a:        &ValidTestFeeConfig,
			b:        func() *FeeConfig { c := ValidTestFeeConfig; c.GasLimit = big.NewInt(1); return &c }(),
			expected: false,
		},
		{
			name:     "not equal nil",
			a:        &ValidTestFeeConfig,
			b:        nil,
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, test.a.Equal(test.b))
		})
	}
}
