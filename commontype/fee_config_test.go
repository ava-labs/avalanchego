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
		expectedError error
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
			expectedError: ErrGasLimitNil,
		},
		{
			name:          "invalid GasLimit in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.GasLimit = big.NewInt(0); return &c }(),
			expectedError: ErrGasLimitTooLow,
		},
		{
			name:          "invalid TargetBlockRate in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.TargetBlockRate = 0; return &c }(),
			expectedError: errTargetBlockRateTooLow,
		},
		{
			name:          "invalid MinBaseFee in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.MinBaseFee = big.NewInt(-1); return &c }(),
			expectedError: errMinBaseFeeNegative,
		},
		{
			name:          "invalid TargetGas in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.TargetGas = big.NewInt(0); return &c }(),
			expectedError: errTargetGasTooLow,
		},
		{
			name:          "invalid BaseFeeChangeDenominator in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.BaseFeeChangeDenominator = big.NewInt(0); return &c }(),
			expectedError: errBaseFeeChangeDenominatorTooLow,
		},
		{
			name:          "invalid MinBlockGasCost in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.MinBlockGasCost = big.NewInt(-1); return &c }(),
			expectedError: errMinBlockGasCostNegative,
		},
		{
			name:          "valid FeeConfig",
			config:        &ValidTestFeeConfig,
			expectedError: nil,
		},
		{
			name: "MinBlockGasCost bigger than MaxBlockGasCost in FeeConfig",
			config: func() *FeeConfig {
				c := ValidTestFeeConfig
				c.MinBlockGasCost = big.NewInt(2)
				c.MaxBlockGasCost = big.NewInt(1)
				return &c
			}(),
			expectedError: ErrMinBlockGasCostTooHigh,
		},
		{
			name:          "invalid BlockGasCostStep in FeeConfig",
			config:        func() *FeeConfig { c := ValidTestFeeConfig; c.BlockGasCostStep = big.NewInt(-1); return &c }(),
			expectedError: errBlockGasCostStepNegative,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Verify()
			require.ErrorIs(t, err, test.expectedError)
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
