// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
)

func TestScalingFromTimeToDouble(t *testing.T) {
	tests := []struct {
		timeToDouble uint64
		want         gas.Gas
	}{
		{timeToDouble: 0, want: 87},
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

// TestGasConfigRoundTrip asserts that [readGasConfig] MUST return the config
// written by [stampGasConfig], and that static pricing MUST be stamped as a
// zero TargetToExcessScaling.
func TestGasConfigRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		config      gastime.GasPriceConfig
		wantScaling uint64
	}{
		{
			name: "dynamic",
			config: gastime.GasPriceConfig{
				TargetToExcessScaling: 100,
				MinPrice:              2,
			},
			wantScaling: 100,
		},
		{
			name: "static",
			config: gastime.GasPriceConfig{
				TargetToExcessScaling: gastime.DefaultTargetToExcessScaling,
				MinPrice:              2,
				StaticPricing:         true,
			},
			wantScaling: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var he customtypes.HeaderExtra
			stampGasConfig(&he, test.config)
			require.Equal(t, test.wantScaling, *he.TargetToExcessScaling, "stampGasConfig() TargetToExcessScaling")

			got, ok := readGasConfig(&he)
			require.True(t, ok, "readGasConfig() ok")
			require.Equal(t, test.config, got, "readGasConfig()")
		})
	}
}
