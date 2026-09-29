// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hookstest

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

// TestBlockTime tests [hook.Points.BlockTime]. withMilliseconds MUST return the
// header with its millisecond timestamp set to ms.
func TestBlockTime(t *testing.T, hooks hook.Points, withMilliseconds func(h *types.Header, ms uint64) *types.Header) {
	tests := []struct {
		name string
		// When TimeMilliseconds is unset, BlockTime falls back to Time's
		// seconds. The VM always sets it, so this decode path is only reachable
		// by exercising the hook directly.
		header    *types.Header
		wantMilli int64
	}{
		{
			name:      "unset_falls_back_to_seconds",
			header:    &types.Header{Time: 1_700_000_000},
			wantMilli: 1_700_000_000_000,
		},
		{
			// TimeMilliseconds encodes 105.500s while Time says 100s (e.g. from
			// a malicious peer). Only the 500ms sub-second remainder is honored.
			name:      "milliseconds_disagreeing_on_second_ignored",
			header:    withMilliseconds(&types.Header{Time: 100}, 105_500),
			wantMilli: 100_500,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hooks.BlockTime(tt.header)
			require.Equal(t, tt.wantMilli, got.UnixMilli(), "hooks.BlockTime().UnixMilli()")
			// Documented invariant: BlockTime(h).Unix() == h.Time, i.e. the
			// second is just the millisecond value floored.
			require.Equal(t, tt.wantMilli/1000, got.Unix(), "hooks.BlockTime().Unix()")
		})
	}
}

// TestSettledBy verifies that [hook.Points.SettledBy] decodes the marker that
// [hook.BlockBuilder.BuildBlock] writes into the header, and returns the zero
// marker when the header carries none. BuildBlock MUST succeed with txs and
// ops.
func TestSettledBy[T hook.Transaction](t *testing.T, hooks hook.PointsG[T], txs []*types.Transaction, ops []T) {
	// built returns the header of a block built carrying the given settled marker.
	built := func(t *testing.T, settled hook.Settled) *types.Header {
		t.Helper()

		block, err := hooks.BuildBlock(
			&types.Header{},
			nil, // blockContext
			txs,
			nil, // receipts
			ops,
			settled,
		)
		require.NoError(t, err, "builder.BuildBlock()")
		return block.Header()
	}

	nonzero := hook.Settled{
		Height:       7,
		GasUnix:      1_000,
		GasNumerator: gas.Gas(3),
		Excess:       gas.Gas(42),
	}
	tests := []struct {
		name   string
		header *types.Header
		want   hook.Settled
	}{
		{
			name:   "absent_marker",
			header: &types.Header{},
			want:   hook.Settled{},
		},
		{
			name:   "zero",
			header: built(t, hook.Settled{}),
			want:   hook.Settled{},
		},
		{
			name:   "nonzero",
			header: built(t, nonzero),
			want:   nonzero,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, hooks.SettledBy(tt.header), "hooks.SettledBy()")
		})
	}
}
