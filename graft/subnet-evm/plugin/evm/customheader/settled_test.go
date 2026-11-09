// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

func TestVerifyNoSAEFields(t *testing.T) {
	tests := []struct {
		name    string
		extra   *customtypes.HeaderExtra
		wantErr error
	}{
		{
			name:    "settled_height_should_fail",
			extra:   &customtypes.HeaderExtra{SettledHeight: utils.PointerTo[uint64](1)},
			wantErr: errSAEFieldSet,
		},
		{
			name:    "settled_gas_unix_should_fail",
			extra:   &customtypes.HeaderExtra{SettledGasUnix: utils.PointerTo[uint64](1)},
			wantErr: errSAEFieldSet,
		},
		{
			name:    "settled_gas_numerator_should_fail",
			extra:   &customtypes.HeaderExtra{SettledGasNumerator: utils.PointerTo[uint64](1)},
			wantErr: errSAEFieldSet,
		},
		{
			name:    "settled_excess_should_fail",
			extra:   &customtypes.HeaderExtra{SettledExcess: utils.PointerTo[uint64](1)},
			wantErr: errSAEFieldSet,
		},
		{
			name: "all_settled_fields_should_fail",
			extra: &customtypes.HeaderExtra{
				SettledHeight:       utils.PointerTo[uint64](1),
				SettledGasUnix:      utils.PointerTo[uint64](2),
				SettledGasNumerator: utils.PointerTo[uint64](3),
				SettledExcess:       utils.PointerTo[uint64](4),
			},
			wantErr: errSAEFieldSet,
		},
		{
			name:    "target_excess_should_fail",
			extra:   &customtypes.HeaderExtra{TargetExcess: utils.PointerTo(gas.Gas(1))},
			wantErr: errSAEFieldSet,
		},
		{
			name:    "gas_config_target_to_excess_scaling_should_fail",
			extra:   &customtypes.HeaderExtra{GasConfigTargetToExcessScaling: utils.PointerTo[uint64](1)},
			wantErr: errSAEFieldSet,
		},
		{
			name:    "gas_config_min_gas_price_should_fail",
			extra:   &customtypes.HeaderExtra{GasConfigMinGasPrice: utils.PointerTo[uint64](1)},
			wantErr: errSAEFieldSet,
		},
		{
			name:    "gas_config_static_pricing_should_fail",
			extra:   &customtypes.HeaderExtra{GasConfigStaticPricing: utils.PointerTo[uint64](1)},
			wantErr: errSAEFieldSet,
		},
		{
			name:  "all_nil_should_work",
			extra: &customtypes.HeaderExtra{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyNoSAEFields(customtypes.WithHeaderExtra(&types.Header{}, tt.extra))
			require.ErrorIs(t, err, tt.wantErr, "VerifyNoSAEFields()")
		})
	}
}
