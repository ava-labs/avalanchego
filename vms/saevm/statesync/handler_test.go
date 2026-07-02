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

func TestLastAccepted(t *testing.T) {
	tests := []struct {
		name      string
		numBlocks uint64
	}{
		{
			name:      "genesis only",
			numBlocks: 0,
		},
		{
			name:      "one block",
			numBlocks: 1,
		},
		{
			name:      "past commit boundary",
			numBlocks: defaultCommitInterval + 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newVM(t)
			vm.acceptBlocks(t, tt.numBlocks)
			h := vm.summaryHandler

			want, err := vm.LastAccepted(t.Context())
			require.NoError(t, err, "VM.LastAccepted()")
			got, err := h.LastAccepted(t.Context())
			require.NoError(t, err, "LastAccepted()")
			require.Equal(t, want, got)
		})
	}
}

func TestBlock(t *testing.T) {
	t.Parallel()

	const numBlocks = defaultCommitInterval + 1
	vm := newVM(t)
	vm.acceptBlocks(t, numBlocks)
	h := vm.summaryHandler

	t.Run("GetBlockIDAtHeight", func(t *testing.T) {
		for height := uint64(0); height <= numBlocks; height++ {
			want, err := vm.GetBlockIDAtHeight(t.Context(), height)
			require.NoErrorf(t, err, "VM.GetBlockIDAtHeight(%d)", height)
			got, err := h.GetBlockIDAtHeight(t.Context(), height)
			require.NoErrorf(t, err, "GetBlockIDAtHeight(%d)", height)
			require.Equalf(t, want, got, "GetBlockIDAtHeight(%d)", height)
		}

		_, err := h.GetBlockIDAtHeight(t.Context(), numBlocks+1)
		require.Equalf(t, database.ErrNotFound, err, "GetBlockIDAtHeight(%d)", numBlocks+1)
	})

	t.Run("GetBlock", func(t *testing.T) {
		for height := uint64(0); height <= numBlocks; height++ {
			want := vm.blockAtHeight(t, height)
			id := ids.ID(want.Hash())
			got, err := h.GetBlock(t.Context(), id)
			require.NoErrorf(t, err, "GetBlock(%s)", id)
			require.Equalf(t, want.Hash(), got.Hash(), "GetBlock(%s).Hash()", id)
			require.Equalf(t, want.Height(), got.Height(), "GetBlock(%s).Height()", id)
		}

		_, err := h.GetBlock(t.Context(), ids.GenerateTestID())
		require.Equal(t, database.ErrNotFound, err, "GetBlock(unknown)")
	})
}

func TestStateSummary(t *testing.T) {
	t.Parallel()

	const numBlocks = defaultCommitInterval + 1
	vm := newVM(t)
	vm.acceptBlocks(t, numBlocks)
	h := vm.summaryHandler
	lastCommitted := vm.blockAtHeight(t, defaultCommitInterval).EthBlock() // last block at a commit boundary

	t.Run("GetLastStateSummary", func(t *testing.T) {
		summary, err := h.GetLastStateSummary(t.Context())
		require.NoError(t, err)
		checkSummaryMatchesBlock(t, summary, lastCommitted)
	})

	t.Run("GetStateSummary_at_committed_height", func(t *testing.T) {
		summary, err := h.GetStateSummary(t.Context(), lastCommitted.NumberU64())
		require.NoError(t, err)
		checkSummaryMatchesBlock(t, summary, lastCommitted)
	})

	t.Run("GetStateSummary_at_uncommitted_height", func(t *testing.T) {
		_, err := h.GetStateSummary(t.Context(), numBlocks)
		require.Equal(t, database.ErrNotFound, err)
	})
}

func TestEmptyStateSummary(t *testing.T) {
	t.Parallel()

	sut := newNetworkedSH(t)
	genesis := sut.genesis.ToBlock()

	t.Run("GetLastStateSummary", func(t *testing.T) {
		summary, err := sut.GetLastStateSummary(t.Context())
		require.NoError(t, err)
		checkSummaryMatchesBlock(t, summary, genesis)
	})

	t.Run("GetStateSummary", func(t *testing.T) {
		summary, err := sut.GetStateSummary(t.Context(), genesis.NumberU64())
		require.NoError(t, err)
		checkSummaryMatchesBlock(t, summary, genesis)
	})
}

func checkSummaryMatchesBlock(t *testing.T, summary *Summary, block *types.Block) {
	t.Helper()

	want := NewSummary(block.Hash(), block.NumberU64())
	if diff := cmp.Diff(want, summary, CmpOpt()); diff != "" {
		t.Errorf("summary mismatch (-want +got):\n%s", diff)
	}
}
