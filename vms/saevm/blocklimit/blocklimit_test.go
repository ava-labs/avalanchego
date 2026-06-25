// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocklimit_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/blocklimit"
)

const (
	lowBlockGasLimit  = 20_000_000 // x/M ~= 9.54 gas/byte
	liveBlockGasLimit = 80_000_000 // x/M ~= 38.15 gas/byte
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
			name:       "nonZeroCalldata_low",
			size:       1_000,
			gasLimit:   16_000,
			blockLimit: lowBlockGasLimit,
			want:       true,
		},
		{
			name:       "zeroByteCalldata_low",
			size:       1_000,
			gasLimit:   4_000,
			blockLimit: lowBlockGasLimit,
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

func TestMinGasForBytes(t *testing.T) {
	tests := []struct {
		name                string
		size, blockGasLimit uint64
		want                uint64
	}{
		{
			name:          "zeroGasLimit_disablesMinimum",
			size:          1_000,
			blockGasLimit: 0,
			want:          0,
		},
		{
			name:          "zeroSize",
			size:          0,
			blockGasLimit: liveBlockGasLimit,
			want:          0,
		},
		{
			name:          "exactDivision",
			size:          10,
			blockGasLimit: blocklimit.MaxBodyBytes,
			want:          10,
		},
		{
			name:          "roundsUp",
			size:          1,
			blockGasLimit: 1,
			want:          1,
		},
		{
			name:          "roundsUpWithRemainder",
			size:          3,
			blockGasLimit: blocklimit.MaxBodyBytes + 1,
			want:          4,
		},
		{
			name:          "sizeAtBudget_cannotFit",
			size:          blocklimit.MaxBodyBytes,
			blockGasLimit: liveBlockGasLimit,
			want:          math.MaxUint64,
		},
		{
			name:          "sizeOverBudget_cannotFit",
			size:          blocklimit.MaxBodyBytes + 1,
			blockGasLimit: liveBlockGasLimit,
			want:          math.MaxUint64,
		},
		{
			name:          "overflows64Bit",
			size:          1_000,
			blockGasLimit: blocklimit.MaxBodyBytes * 1_000_000_000_000,
			want:          1_000_000_000_000_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blocklimit.MinGasForBytes(tt.size, tt.blockGasLimit)
			require.Equal(
				t,
				tt.want,
				got,
				"MinGasForBytes(size=%d, blockGasLimit=%d)",
				tt.size,
				tt.blockGasLimit,
			)
		})
	}
}
