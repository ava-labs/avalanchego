// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontype

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestGasPriceConfigVerify(t *testing.T) {
	tests := []struct {
		name   string
		config GasPriceConfig
		want   error
	}{
		{
			name:   "valid config",
			config: DefaultGasPriceConfig(),
		},
		{
			name: "valid with validatorTargetGas",
			config: GasPriceConfig{
				ValidatorTargetGas: true,
				MinGasPrice:        1,
				TimeToDouble:       60,
			},
		},
		{
			name: "valid with staticPricing",
			config: GasPriceConfig{
				TargetGas:     MinTargetGas,
				StaticPricing: true,
				MinGasPrice:   1,
			},
		},
		{
			name: "valid with both validatorTargetGas and staticPricing",
			config: GasPriceConfig{
				ValidatorTargetGas: true,
				StaticPricing:      true,
				MinGasPrice:        1,
			},
		},
		{
			name: "minGasPrice zero",
			config: GasPriceConfig{
				TargetGas:    MinTargetGas,
				TimeToDouble: 60,
			},
			want: ErrMinGasPriceTooLow,
		},
		{
			name: "targetGas must be zero when validatorTargetGas is true",
			config: GasPriceConfig{
				ValidatorTargetGas: true,
				TargetGas:          MinTargetGas,
				MinGasPrice:        1,
				TimeToDouble:       60,
			},
			want: errTargetGasMustBeZero,
		},
		{
			name: "targetGas below minimum",
			config: GasPriceConfig{
				TargetGas:    MinTargetGas - 1,
				MinGasPrice:  1,
				TimeToDouble: 60,
			},
			want: errTargetGasBelowMin,
		},
		{
			name: "targetGas at minimum boundary",
			config: GasPriceConfig{
				TargetGas:    MinTargetGas,
				MinGasPrice:  1,
				TimeToDouble: 1,
			},
		},
		{
			name: "timeToDouble must be zero when staticPricing is true",
			config: GasPriceConfig{
				TargetGas:     MinTargetGas,
				StaticPricing: true,
				MinGasPrice:   1,
				TimeToDouble:  60,
			},
			want: errTimeToDoubleMustBeZero,
		},
		{
			name: "timeToDouble must be positive when staticPricing is false",
			config: GasPriceConfig{
				TargetGas:   MinTargetGas,
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

// TestGasPriceConfigPackFormat asserts exact packed bytes for known configs.
// This catches backward-incompatible format changes that round-trip tests miss.
func TestGasPriceConfigPackFormat(t *testing.T) {
	tests := []struct {
		name   string
		config GasPriceConfig
		want   common.Hash
	}{
		{
			name:   "default config",
			config: DefaultGasPriceConfig(),
			want:   common.HexToHash("0x0000000000000f4240000000000000000001000000000000003c000000000000"),
		},
		{
			name: "all flags true and max uint64",
			config: GasPriceConfig{
				ValidatorTargetGas: true,
				TargetGas:          math.MaxUint64,
				StaticPricing:      true,
				MinGasPrice:        math.MaxUint64,
				TimeToDouble:       math.MaxUint64,
			},
			want: common.HexToHash("0x01ffffffffffffffff01ffffffffffffffffffffffffffffffff000000000000"),
		},
		{
			name: "validatorTargetGas mode",
			config: GasPriceConfig{
				ValidatorTargetGas: true,
				MinGasPrice:        1,
				TimeToDouble:       60,
			},
			want: common.HexToHash("0x010000000000000000000000000000000001000000000000003c000000000000"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.config.Pack(), "Pack")
		})
	}
}

func FuzzGasPriceConfigPacking(f *testing.F) {
	f.Add(false, false, uint64(0), uint64(0), uint64(0))
	f.Add(true, true, uint64(math.MaxUint64), uint64(math.MaxUint64), uint64(math.MaxUint64))
	f.Add(false, false, MinTargetGas, uint64(1), uint64(60))
	f.Add(true, false, uint64(0), uint64(1), uint64(60))
	f.Add(false, true, MinTargetGas, uint64(1), uint64(0))
	f.Add(true, true, uint64(0), uint64(1), uint64(0))

	f.Fuzz(func(t *testing.T, validator, static bool, target, minGas, double uint64) {
		in := &GasPriceConfig{validator, target, static, minGas, double}
		got := new(GasPriceConfig)
		got.UnpackFrom(in.Pack())
		require.Equalf(t, *in, *got, "%T.UnpackFrom(%[1]T.Pack()) round trip", in)
		require.Truef(t, got.Equal(in), "%T.Equal([packed original])", got)
	})
}

func TestGasPriceConfigEqual(t *testing.T) {
	tests := []struct {
		name string
		a    *GasPriceConfig
		b    *GasPriceConfig
		want bool
	}{
		{
			name: "both equal",
			a:    utils.PointerTo(DefaultGasPriceConfig()),
			b:    utils.PointerTo(DefaultGasPriceConfig()),
			want: true,
		},
		{
			name: "different targetGas",
			a:    utils.PointerTo(DefaultGasPriceConfig()),
			b: func() *GasPriceConfig {
				c := DefaultGasPriceConfig()
				c.TargetGas++
				return &c
			}(),
			want: false,
		},
		{
			name: "other nil",
			a:    utils.PointerTo(DefaultGasPriceConfig()),
			b:    nil,
			want: false,
		},
		{
			name: "receiver nil",
			a:    nil,
			b:    utils.PointerTo(DefaultGasPriceConfig()),
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
