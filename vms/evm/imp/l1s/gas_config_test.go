// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
)

func TestScalingFromTimeToDouble(t *testing.T) {
	tests := []struct {
		timeToDouble uint64
		want         uint64
	}{
		{timeToDouble: 0, want: 0},
		{timeToDouble: 1, want: 1},
		{timeToDouble: 60, want: 87},
		{timeToDouble: 12_721_892_464_627_276_976, want: math.MaxUint64},
		{timeToDouble: 12_721_892_464_627_276_977, want: math.MaxUint64},
	}
	for _, test := range tests {
		require.Equal(
			t,
			test.want,
			scalingFromTimeToDouble(test.timeToDouble),
			"scalingFromTimeToDouble(%d)",
			test.timeToDouble,
		)
	}
}

// TestGasConfigRoundTrip asserts that [hooks.GasConfigAfter] MUST return the
// price config written by [setGasConfig].
func TestGasConfigRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		stored commontype.GasPriceConfig
		want   gastime.GasPriceConfig
	}{
		{
			name:   "disabled",
			stored: disabledGasConfig(),
			want:   gastime.DefaultGasPriceConfig(),
		},
		{
			name: "dynamic",
			stored: commontype.GasPriceConfig{
				ValidatorTargetGas: true,
				MinGasPrice:        2,
				TimeToDouble:       120,
			},
			want: gastime.GasPriceConfig{
				TargetToExcessScaling: 174,
				MinPrice:              2,
			},
		},
		{
			name: "static",
			stored: commontype.GasPriceConfig{
				ValidatorTargetGas: true,
				MinGasPrice:        2,
				StaticPricing:      true,
			},
			want: gastime.GasPriceConfig{
				TargetToExcessScaling: gastime.DefaultTargetToExcessScaling,
				MinPrice:              2,
				StaticPricing:         true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := customtypes.WithHeaderExtra(&types.Header{}, &customtypes.HeaderExtra{})
			setGasConfig(customtypes.GetHeaderExtra(header), test.stored)

			_, got := (*hooks)(nil).GasConfigAfter(header)
			require.Equal(t, test.want, got, "GasConfigAfter()")
		})
	}
}
