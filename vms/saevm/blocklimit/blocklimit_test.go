// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocklimit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/blocklimit"
)

const (
	floorBlockGasLimit = 20_000_000 // x/M ~= 9.54 gas/byte
	liveBlockGasLimit  = 80_000_000 // x/M ~= 38.15 gas/byte
)

func TestEligible(t *testing.T) {
	tests := []struct {
		name                       string
		size, gasLimit, blockLimit uint64
		want                       bool
	}{
		{
			name:       "zeroByteCalldata_live",
			size:       1_000,
			gasLimit:   4_000,
			blockLimit: liveBlockGasLimit,
		},
		{
			name:       "nonZeroCalldata_live",
			size:       1_000,
			gasLimit:   16_000,
			blockLimit: liveBlockGasLimit,
		},
		{
			name:       "typicalTransfer_live",
			size:       1_000,
			gasLimit:   200_000,
			blockLimit: liveBlockGasLimit,
			want:       true,
		},
		{
			name:       "minTransfer_live",
			size:       110,
			gasLimit:   21_000,
			blockLimit: liveBlockGasLimit,
			want:       true,
		},
		{
			name:       "nonZeroCalldata_floor",
			size:       1_000,
			gasLimit:   16_000,
			blockLimit: floorBlockGasLimit,
			want:       true,
		},
		{
			name:       "zeroByteCalldata_floor",
			size:       1_000,
			gasLimit:   4_000,
			blockLimit: floorBlockGasLimit,
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
			blockLimit: liveBlockGasLimit,
			want:       true,
		},
		{
			name:       "largeSizeAndBlockLimit",
			size:       1 << 32,
			gasLimit:   1,
			blockLimit: 1 << 32,
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
