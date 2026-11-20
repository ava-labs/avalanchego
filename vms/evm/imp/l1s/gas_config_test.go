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
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
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
// target and price config written by [setGasConfig].
func TestGasConfigRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		stored     commontype.GasPriceConfig
		wantTarget gas.Gas
		wantConfig gastime.GasPriceConfig
	}{
		{
			name:       "default",
			stored:     commontype.DefaultGasPriceConfig(),
			wantTarget: dynamic.MinTarget,
			wantConfig: gastime.DefaultGasPriceConfig(),
		},
		{
			name: "pinned_target",
			stored: commontype.GasPriceConfig{
				TargetGas:    20_000_000,
				MinGasPrice:  2,
				TimeToDouble: 120,
			},
			wantTarget: 20_000_000,
			wantConfig: gastime.GasPriceConfig{
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
			wantTarget: dynamic.MinTarget,
			wantConfig: gastime.GasPriceConfig{
				TargetToExcessScaling: gastime.DefaultTargetToExcessScaling,
				MinPrice:              2,
				StaticPricing:         true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			extra := new(customtypes.HeaderExtra)
			setGasConfig(extra, test.stored)
			header := customtypes.WithHeaderExtra(new(types.Header), extra)

			gotTarget, gotConfig := new(hooks).GasConfigAfter(header)
			require.Equal(t, test.wantTarget, gotTarget, "target")
			require.Equal(t, test.wantConfig, gotConfig, "config")
		})
	}
}
