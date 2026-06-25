// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils"
)

func settledHeader(extra *customtypes.HeaderExtra) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{},
		extra,
	)
}

func TestVerifySettled(t *testing.T) {
	tests := []struct {
		name    string
		header  *types.Header
		wantErr error
	}{
		{
			name:    "settled_height_should_fail",
			header:  settledHeader(&customtypes.HeaderExtra{SettledHeight: utils.PointerTo[uint64](1)}),
			wantErr: errSettledMarkerSet,
		},
		{
			name:    "settled_gas_unix_should_fail",
			header:  settledHeader(&customtypes.HeaderExtra{SettledGasUnix: utils.PointerTo[uint64](1)}),
			wantErr: errSettledMarkerSet,
		},
		{
			name:    "settled_gas_numerator_should_fail",
			header:  settledHeader(&customtypes.HeaderExtra{SettledGasNumerator: utils.PointerTo[uint64](1)}),
			wantErr: errSettledMarkerSet,
		},
		{
			name:    "settled_excess_should_fail",
			header:  settledHeader(&customtypes.HeaderExtra{SettledExcess: utils.PointerTo[uint64](1)}),
			wantErr: errSettledMarkerSet,
		},
		{
			name: "all_settled_fields_should_fail",
			header: settledHeader(&customtypes.HeaderExtra{
				SettledHeight:       utils.PointerTo[uint64](1),
				SettledGasUnix:      utils.PointerTo[uint64](2),
				SettledGasNumerator: utils.PointerTo[uint64](3),
				SettledExcess:       utils.PointerTo[uint64](4),
			}),
			wantErr: errSettledMarkerSet,
		},
		{
			name:   "all_nil_should_work",
			header: settledHeader(&customtypes.HeaderExtra{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifySettled(tt.header)
			require.ErrorIs(t, err, tt.wantErr, "VerifySettled()")
		})
	}
}
