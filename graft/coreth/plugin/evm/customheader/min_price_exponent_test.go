// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

func TestVerifyMinPriceExponent(t *testing.T) {
	tests := []struct {
		name    string
		header  *types.Header
		wantErr error
	}{
		{
			name:   "unset",
			header: &types.Header{Time: 1001},
		},
		{
			name:    "rejects_set_value",
			header:  headerWithMinPriceExponent(1001, 1000),
			wantErr: errRemoteMinPriceExponentSet,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyMinPriceExponent(test.header)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func headerWithMinPriceExponent(time uint64, exponent dynamic.PriceExponent) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{Time: time},
		&customtypes.HeaderExtra{MinPriceExponent: &exponent},
	)
}
