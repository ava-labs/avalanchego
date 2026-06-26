// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocklimit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/blocklimit"
)

func TestEligible(t *testing.T) {
	const (
		strict = 80_000_000 // x/M ~= 38.15 gas/byte, Helicon's initial gas limit
		loose  = 20_000_000 // x/M ~= 9.54 gas/byte
	)

	tests := []struct {
		name                       string
		size, gasLimit, blockLimit uint64
		want                       bool
	}{
		{
			name:       "zeroByteCalldata_strict",
			size:       1_000,
			gasLimit:   4_000,
			blockLimit: strict,
		},
		{
			name:       "nonZeroCalldata_strict",
			size:       1_000,
			gasLimit:   16_000,
			blockLimit: strict,
		},
		{
			name:       "typicalTransfer_strict",
			size:       1_000,
			gasLimit:   200_000,
			blockLimit: strict,
			want:       true,
		},
		{
			name:       "minTransfer_strict",
			size:       110,
			gasLimit:   21_000,
			blockLimit: strict,
			want:       true,
		},
		{
			name:       "nonZeroCalldata_loose",
			size:       1_000,
			gasLimit:   16_000,
			blockLimit: loose,
			want:       true,
		},
		{
			name:       "zeroByteCalldata_loose",
			size:       1_000,
			gasLimit:   4_000,
			blockLimit: loose,
		},
		{
			name:       "boundaryEqual",
			size:       100,
			gasLimit:   100,
			blockLimit: blocklimit.MaxBlockBytes,
			want:       true,
		},
		{
			name:       "boundaryOneByteOver",
			size:       101,
			gasLimit:   100,
			blockLimit: blocklimit.MaxBlockBytes,
		},
		{
			name:       "boundaryOneGasOver",
			size:       100,
			gasLimit:   101,
			blockLimit: blocklimit.MaxBlockBytes,
			want:       true,
		},
		{
			name:       "largeGasLimit",
			size:       1_000,
			gasLimit:   1 << 43,
			blockLimit: strict,
			want:       true,
		},
		{
			name:       "largeSizeAndBlockLimit",
			size:       1 << 32,
			gasLimit:   1,
			blockLimit: 1 << 32,
		},
		{
			name:     "zeroBlockGasLimit",
			size:     100,
			gasLimit: 1_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blocklimit.Eligible(tt.size, tt.gasLimit, tt.blockLimit)
			require.Equal(
				t,
				tt.want,
				got,
				"Eligible(size=%d, gasLimit=%d, blockGasLimit=%d)",
				tt.size,
				tt.gasLimit,
				tt.blockLimit,
			)
		})
	}
}
