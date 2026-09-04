// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, saetest.GoleakOptions()...)
}

func TestBlock(t *testing.T) {
	t.Parallel()

	const numBlocks uint64 = defaultCommitInterval + 1
	vm := newVM(t)
	vm.acceptBlocks(t, numBlocks)
	h := vm.Handler

	t.Run("GetBlockIDAtHeight", func(t *testing.T) {
		for height := range numBlocks + 1 {
			want, err := vm.vm.GetBlockIDAtHeight(t.Context(), height)
			require.NoErrorf(t, err, "VM.GetBlockIDAtHeight(%d)", height)
			got, err := h.GetBlockIDAtHeight(t.Context(), height)
			require.NoErrorf(t, err, "GetBlockIDAtHeight(%d)", height)
			require.Equalf(t, want, got, "GetBlockIDAtHeight(%d)", height)
		}

		_, err := h.GetBlockIDAtHeight(t.Context(), numBlocks+1)
		require.Equalf(t, database.ErrNotFound, err, "GetBlockIDAtHeight(%d)", numBlocks+1)
	})

	t.Run("GetBlock", func(t *testing.T) {
		for height := range numBlocks + 1 {
			want := vm.blockAtHeight(t, height)
			id := ids.ID(want.Hash())
			got, err := h.GetBlock(t.Context(), id)
			require.NoErrorf(t, err, "GetBlock(%s): %d", id, height)
			require.Equalf(t, want.Hash(), got.Hash(), "GetBlock(%s).Hash(): %d", id, height)
			require.Equalf(t, want.Height(), got.Height(), "GetBlock(%s).Height()", id)
		}

		_, err := h.GetBlock(t.Context(), ids.GenerateTestID())
		require.Equal(t, database.ErrNotFound, err, "GetBlock(unknown)")
	})
}

// TestGetStateSummary asserts that a summary is served only for an
// asynchronous block at a committed height.
func TestGetStateSummary(t *testing.T) {
	t.Parallel()

	const (
		lastCommitted = defaultCommitInterval
		numBlocks     = defaultCommitInterval + 1
	)

	tests := []struct {
		name            string
		numBlocks       uint64
		lastSynchronous uint64
		height          uint64
		wantErr         error
	}{
		{
			name:      "committed_height",
			numBlocks: numBlocks,
			height:    lastCommitted,
		},
		{
			name:      "uncommitted_height",
			numBlocks: numBlocks,
			height:    numBlocks,
			wantErr:   database.ErrNotFound,
		},
		{
			name:      "unknown_committed_height",
			numBlocks: numBlocks,
			height:    2 * defaultCommitInterval,
			wantErr:   database.ErrNotFound,
		},
		{
			name:      "genesis",
			numBlocks: numBlocks,
			height:    0,
			wantErr:   database.ErrNotFound,
		},
		{
			name:            "synchronous_committed_height",
			numBlocks:       numBlocks,
			lastSynchronous: lastCommitted,
			height:          lastCommitted,
			wantErr:         database.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newVM(t, withLastSynchronous(tt.lastSynchronous))
			vm.acceptBlocks(t, tt.numBlocks)

			summary, err := vm.Handler.GetStateSummary(t.Context(), tt.height)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}
			checkSummaryMatchesBlock(t, summary, vm.blockAtHeight(t, tt.height).EthBlock())
		})
	}
}

// TestGetLastStateSummary asserts that the last summary is served only if the
// block at the last committed height is asynchronous.
func TestGetLastStateSummary(t *testing.T) {
	t.Parallel()

	const (
		lastCommitted = defaultCommitInterval
		numBlocks     = defaultCommitInterval + 1
	)

	tests := []struct {
		name            string
		numBlocks       uint64
		lastSynchronous uint64
		wantHeight      uint64
		wantErr         error
	}{
		{
			name:       "last_committed",
			numBlocks:  numBlocks,
			wantHeight: lastCommitted,
		},
		{
			name:    "only_genesis",
			wantErr: database.ErrNotFound,
		},
		{
			name:            "last_committed_synchronous",
			numBlocks:       numBlocks,
			lastSynchronous: lastCommitted,
			wantErr:         database.ErrNotFound,
		},
		{
			name:            "last_committed_above_synchronous_threshold",
			numBlocks:       numBlocks,
			lastSynchronous: lastCommitted - 1,
			wantHeight:      lastCommitted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newVM(t, withLastSynchronous(tt.lastSynchronous))
			vm.acceptBlocks(t, tt.numBlocks)

			summary, err := vm.Handler.GetLastStateSummary(t.Context())
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}
			checkSummaryMatchesBlock(t, summary, vm.blockAtHeight(t, tt.wantHeight).EthBlock())
		})
	}
}

func checkSummaryMatchesBlock(t *testing.T, summary *Summary, block *types.Block) {
	t.Helper()

	want := NewSummary(block.Hash(), block.NumberU64())
	if diff := cmp.Diff(want, summary, CmpOpt()); diff != "" {
		t.Errorf("summary mismatch (-want +got):\n%s", diff)
	}
}
