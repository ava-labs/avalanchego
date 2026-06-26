// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocklimit_test

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/blocklimit"
)

func TestEligible(t *testing.T) {
	tx := types.NewTx(&types.LegacyTx{Gas: 21_000})
	y, g := tx.Size(), tx.Gas()

	tests := []struct {
		name                    string
		blockGasLimit, maxBytes uint64
		want                    bool
	}{
		{
			name:          "atBoundary",
			blockGasLimit: g,
			maxBytes:      y,
			want:          true,
		},
		{
			name:          "blockGasLimitOneOver",
			blockGasLimit: g + 1,
			maxBytes:      y,
		},
		{
			name:          "maxBytesOneOver",
			blockGasLimit: g,
			maxBytes:      y + 1,
			want:          true,
		},
		{
			name:          "blockGasLimitDoubled",
			blockGasLimit: 2 * g,
			maxBytes:      y,
		},
		{
			name:          "maxBytesDoubled",
			blockGasLimit: g,
			maxBytes:      2 * y,
			want:          true,
		},
		{
			name:          "overflowingProductRejected",
			blockGasLimit: math.MaxUint64,
			maxBytes:      1,
		},
		{
			name:          "overflowingProductAccepted",
			blockGasLimit: 1,
			maxBytes:      math.MaxUint64,
			want:          true,
		},
		{
			name:     "zeroBlockGasLimit",
			maxBytes: y,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blocklimit.Eligible(tx, tt.blockGasLimit, tt.maxBytes)
			require.Equal(
				t,
				tt.want,
				got,
				"Eligible(size=%d, gasLimit=%d, blockGasLimit=%d, maxBytes=%d)",
				y, // size
				g, // gasLimit
				tt.blockGasLimit,
				tt.maxBytes,
			)
		})
	}
}
