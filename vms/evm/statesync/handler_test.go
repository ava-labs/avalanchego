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
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
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
			name:      "below_first_summary_height",
			numBlocks: defaultCommitInterval - 1,
			wantErr:   database.ErrNotFound,
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

// TestGetLastStateSummaryBeforeRegisterHandlers asserts that a summary isn't
// advertised by a handler that hasn't registered its state sync handlers.
func TestGetLastStateSummaryBeforeRegisterHandlers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	vm := newVM(t)
	vm.acceptBlocks(t, defaultCommitInterval+1)
	_, err := vm.Handler.GetLastStateSummary(ctx)
	require.NoError(t, err, "GetLastStateSummary() after RegisterHandlers()")

	unregistered := newSUT(t, withDiskOf(vm.sut))
	_, err = unregistered.GetLastStateSummary(ctx)
	require.ErrorIs(t, err, database.ErrNotFound, "GetLastStateSummary() before RegisterHandlers()")
}

// TestGetLastStateSummaryAfterRestart asserts that a Firewood node, which loses
// its revision history when it shuts down, doesn't advertise a summary until it
// can serve one again.
func TestGetLastStateSummaryAfterRestart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	vm := newVM(t, withScheme(firewood.Scheme))
	vm.acceptBlocks(t, defaultCommitInterval+1)

	summary, err := vm.Handler.GetLastStateSummary(ctx)
	require.NoError(t, err, "GetLastStateSummary() before restart")
	checkSummaryMatchesBlock(t, summary, vm.blockAtHeight(t, defaultCommitInterval).EthBlock())

	restarted := vm.restart(t)

	// The last summary is still at the same height, but its root predates
	// the restart.
	_, err = restarted.Handler.GetLastStateSummary(ctx)
	require.ErrorIs(t, err, database.ErrNotFound, "GetLastStateSummary() after restart")

	const nextHeight = 2 * defaultCommitInterval
	for h := restarted.lastAcceptedBlock(t).Height() + 1; h < nextHeight; h++ {
		restarted.acceptBlocks(t, 1)
		_, err = restarted.Handler.GetLastStateSummary(ctx)
		require.ErrorIsf(t, err, database.ErrNotFound, "GetLastStateSummary() at height %d", h)
	}

	// Executing the block at the next summary height commits its root again.
	restarted.acceptBlocks(t, 1)
	summary, err = restarted.Handler.GetLastStateSummary(ctx)
	require.NoError(t, err, "GetLastStateSummary() after the next summary height")
	checkSummaryMatchesBlock(t, summary, restarted.blockAtHeight(t, nextHeight).EthBlock())
}
