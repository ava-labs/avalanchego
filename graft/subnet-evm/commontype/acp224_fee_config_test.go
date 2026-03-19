// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontype

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestACP224FeeConfigVerify(t *testing.T) {
	tests := []struct {
		name   string
		config ACP224FeeConfig
		want   error
	}{
		{
			name:   "valid config",
			config: ValidTestACP224FeeConfig,
		},
		{
			name: "valid with validatorTargetGas",
			config: ACP224FeeConfig{
				ValidatorTargetGas: true,
				MinGasPrice:        1,
				TimeToDouble:       60,
			},
		},
		{
			name: "valid with staticPricing",
			config: ACP224FeeConfig{
				TargetGas:     MinTargetGasACP224,
				StaticPricing: true,
				MinGasPrice:   1,
			},
		},
		{
			name: "valid with both validatorTargetGas and staticPricing",
			config: ACP224FeeConfig{
				ValidatorTargetGas: true,
				StaticPricing:      true,
				MinGasPrice:        1,
			},
		},
		{
			name: "minGasPrice zero",
			config: ACP224FeeConfig{
				TargetGas:    MinTargetGasACP224,
				TimeToDouble: 60,
			},
			want: ErrMinGasPriceTooLow,
		},
		{
			name: "targetGas must be zero when validatorTargetGas is true",
			config: ACP224FeeConfig{
				ValidatorTargetGas: true,
				TargetGas:          MinTargetGasACP224,
				MinGasPrice:        1,
				TimeToDouble:       60,
			},
			want: ErrTargetGasMustBeZero,
		},
		{
			name: "targetGas below minimum",
			config: ACP224FeeConfig{
				TargetGas:    MinTargetGasACP224 - 1,
				MinGasPrice:  1,
				TimeToDouble: 60,
			},
			want: ErrTargetGasTooLowACP224,
		},
		{
			name: "targetGas at minimum boundary",
			config: ACP224FeeConfig{
				TargetGas:    MinTargetGasACP224,
				MinGasPrice:  1,
				TimeToDouble: 1,
			},
		},
		{
			name: "timeToDouble must be zero when staticPricing is true",
			config: ACP224FeeConfig{
				TargetGas:     MinTargetGasACP224,
				StaticPricing: true,
				MinGasPrice:   1,
				TimeToDouble:  60,
			},
			want: ErrTimeToDoubleMustBeZero,
		},
		{
			name: "timeToDouble must be positive when staticPricing is false",
			config: ACP224FeeConfig{
				TargetGas:   MinTargetGasACP224,
				MinGasPrice: 1,
			},
			want: ErrTimeToDoubleTooLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Verify()
			require.ErrorIs(t, err, tt.want, "Verify")
		})
	}
}

func TestACP224FeeConfigEqual(t *testing.T) {
	tests := []struct {
		name string
		a    *ACP224FeeConfig
		b    *ACP224FeeConfig
		want bool
	}{
		{
			name: "both equal",
			a:    &ValidTestACP224FeeConfig,
			b: &ACP224FeeConfig{
				TargetGas:    15_000_000,
				MinGasPrice:  1,
				TimeToDouble: 60,
			},
			want: true,
		},
		{
			name: "different targetGas",
			a:    &ValidTestACP224FeeConfig,
			b: func() *ACP224FeeConfig {
				c := ValidTestACP224FeeConfig
				c.TargetGas = 20_000_000
				return &c
			}(),
			want: false,
		},
		{
			name: "other nil",
			a:    &ValidTestACP224FeeConfig,
			b:    nil,
			want: false,
		},
		{
			name: "receiver nil",
			a:    nil,
			b:    &ValidTestACP224FeeConfig,
			want: false,
		},
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.a.Equal(tt.b), "Equal()")
		})
	}
}
