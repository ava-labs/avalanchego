// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
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

// TestSetGasConfig asserts that [setGasConfig] MUST write c into the header,
// setting TargetExponent only if c pins the target.
func TestSetGasConfig(t *testing.T) {
	tests := []struct {
		name                      string
		stored                    commontype.GasPriceConfig
		wantTargetExponent        *dynamic.TargetExponent
		wantTargetToExcessScaling uint64
		wantMinPrice              uint64
	}{
		{
			name:                      "default",
			stored:                    commontype.DefaultGasPriceConfig(),
			wantTargetToExcessScaling: 87,
			wantMinPrice:              1,
		},
		{
			name: "pinned_target",
			stored: commontype.GasPriceConfig{
				TargetGas:    20_000_000,
				MinGasPrice:  2,
				TimeToDouble: 120,
			},
			wantTargetExponent:        new(dynamic.DesiredTargetExponent(20_000_000)),
			wantTargetToExcessScaling: 174,
			wantMinPrice:              2,
		},
		{
			name: "static",
			stored: commontype.GasPriceConfig{
				ValidatorTargetGas: true,
				MinGasPrice:        2,
				StaticPricing:      true,
			},
			wantTargetToExcessScaling: 0,
			wantMinPrice:              2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			extra := new(customtypes.HeaderExtra)
			setGasConfig(extra, test.stored)

			require.Equal(t, test.wantTargetExponent, extra.TargetExponent, "TargetExponent")
			require.Equal(t, &test.wantTargetToExcessScaling, extra.TargetToExcessScaling, "TargetToExcessScaling")
			require.Equal(t, &test.wantMinPrice, extra.MinPrice, "MinPrice")
		})
	}
}
