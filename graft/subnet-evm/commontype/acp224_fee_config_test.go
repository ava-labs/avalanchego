// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontype

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestACP224FeeConfigVerify(t *testing.T) {
	tests := []struct {
		name   string
		config ACP224FeeConfig
		want   error
	}{
		{
			name:   "valid config",
			config: DefaultACP224FeeConfig(),
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
			want: errTargetGasMustBeZero,
		},
		{
			name: "targetGas below minimum",
			config: ACP224FeeConfig{
				TargetGas:    MinTargetGasACP224 - 1,
				MinGasPrice:  1,
				TimeToDouble: 60,
			},
			want: errTargetGasTooLowACP224,
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
			want: errTimeToDoubleMustBeZero,
		},
		{
			name: "timeToDouble must be positive when staticPricing is false",
			config: ACP224FeeConfig{
				TargetGas:   MinTargetGasACP224,
				MinGasPrice: 1,
			},
			want: errTimeToDoubleTooLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Verify()
			require.ErrorIs(t, err, tt.want, "Verify")
		})
	}
}

func FuzzACP224FeeConfigPacking(f *testing.F) {
	for _, v := range []bool{true, false} {
		for _, s := range []bool{true, false} {
			for t := range uint64(3) {
				for m := range uint64(3) {
					for d := range uint64(3) {
						f.Add(v, s, t, m, d)
					}
				}
			}
		}
	}

	f.Fuzz(func(t *testing.T, validator, static bool, target, minGas, double uint64) {
		in := &ACP224FeeConfig{validator, target, static, minGas, double}
		got := new(ACP224FeeConfig)
		got.UnpackFrom(in.Pack())
		require.Equalf(t, *in, *got, "%T.UnpackFrom(%[1]T.Pack()) round trip", in)
		require.Truef(t, got.Equal(in), "%T.Equal([packed original])", got)
	})
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
			a:    utils.PointerTo(DefaultACP224FeeConfig()),
			b:    utils.PointerTo(DefaultACP224FeeConfig()),
			want: true,
		},
		{
			name: "different targetGas",
			a:    utils.PointerTo(DefaultACP224FeeConfig()),
			b: func() *ACP224FeeConfig {
				c := DefaultACP224FeeConfig()
				c.TargetGas++
				return &c
			}(),
			want: false,
		},
		{
			name: "other nil",
			a:    utils.PointerTo(DefaultACP224FeeConfig()),
			b:    nil,
			want: false,
		},
		{
			name: "receiver nil",
			a:    nil,
			b:    utils.PointerTo(DefaultACP224FeeConfig()),
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
			require.Equalf(t, tt.want, tt.a.Equal(tt.b), "%T(%+v).Equal(%+v)", tt.a, tt.a, tt.b)
		})
	}
}
