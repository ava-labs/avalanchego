// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/sync/leaf"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
)

// A storage trie split into concurrent segments reconstructs via snapshot re-read.
func TestStateTrie_SegmentedStorageReconstruct(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	account := common.HexToHash("0xac")
	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrieDistributed(t, trieDB, 3000)

	fetcher := synctest.ServeLeaves(t, ctx, trieDB)

	target := rawdb.NewMemoryDatabase()
	leaves := newStorageLeafStore(target, []common.Hash{account})

	tasks := make(chan leaf.Task, 64)
	st, err := newStateTrie(target, root, account, leaves, stateTrieConfig{
		numSegments: numStorageTrieSegments,
		threshold:   1,
		tasks:       tasks,
		onDone:      func(context.Context) error { close(tasks); return nil },
	})
	require.NoError(t, err)
	tasks <- st.segments[0]

	require.NoError(t, leaf.NewSyncer(fetcher, tasks, leaf.WithNumWorkers(4)).Sync(ctx))

	require.Greater(t, len(st.segments), 1, "the storage trie must have split into segments")
	requireReconstructed(t, target, root, keys, vals)
	for i, k := range keys {
		require.Equal(t, vals[i], rawdb.ReadStorageSnapshot(target, account, common.BytesToHash(k)))
	}
}

// requireReconstructed reads every pair back through the rebuilt trie.
func requireReconstructed(t *testing.T, target ethdb.Database, root common.Hash, keys, vals [][]byte) {
	t.Helper()
	tr, err := trie.New(trie.TrieID(root), triedb.NewDatabase(target, nil))
	require.NoError(t, err)
	for i, k := range keys {
		got, err := tr.Get(k)
		require.NoError(t, err)
		require.Equal(t, vals[i], got)
	}
}

// The estimate divides by the progress made, so a segment that has not moved must
// report unknown rather than divide by zero.
func TestStateSegment_EstimateSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		start, pos, end []byte
		leafCount       uint64
		want            uint64
	}{
		{
			name:      "no_progress_is_unknown",
			start:     []byte{0x00, 0x00},
			end:       []byte{0xff, 0xff},
			leafCount: 10,
		},
		{
			name:      "pos_back_at_start_is_unknown",
			start:     []byte{0x10, 0x00},
			pos:       []byte{0x10, 0x00},
			end:       []byte{0x20, 0x00},
			leafCount: 10,
		},
		{
			name:      "halfway_doubles_the_count",
			start:     []byte{0x00, 0x00},
			pos:       []byte{0x80, 0x00},
			end:       []byte{0xff, 0xff},
			leafCount: 100,
			want:      99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &stateSegment{start: tt.start, pos: tt.pos, end: tt.end, leafCount: tt.leafCount}
			require.Equal(t, tt.want, s.estimateSize())
		})
	}
}

// A failed leaf write must abort the segment rather than advance its resume
// position over leaves that never landed.
func TestStateSegment_OnLeavesPropagatesWriteError(t *testing.T) {
	t.Parallel()

	target := rawdb.NewMemoryDatabase()
	seg := &stateSegment{
		trie:  &stateTrie{leaves: newStorageLeafStore(target, []common.Hash{{1}})},
		batch: target.NewBatch(),
		start: []byte{0x00, 0x00},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := seg.OnLeaves(ctx, evmstate.Leaves{
		Keys: [][]byte{common.Hash{9}.Bytes()},
		Vals: [][]byte{{1}},
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, seg.pos, "a failed write must not advance the resume position")
}

// Hashing re-reads the snapshot leaf by leaf, so a cancel there must abort the
// trie rather than commit a root built from a partial re-read.
func TestStateTrie_HashSegmentHonoursCancel(t *testing.T) {
	t.Parallel()

	account := common.Hash{0xac}
	target := rawdb.NewMemoryDatabase()
	leaves := newStorageLeafStore(target, []common.Hash{account})

	tr := &stateTrie{
		db:           target,
		leaves:       leaves,
		batch:        target.NewBatch(),
		stackTrie:    trie.NewStackTrie(nil),
		segmentsDone: make(map[int]struct{}),
	}
	seg := &stateSegment{trie: tr, batch: target.NewBatch(), start: []byte{0x00, 0x00}}
	tr.segments = []*stateSegment{seg}

	// A leaf must already be in the snapshot, or the re-read loop never runs.
	require.NoError(t, leaves.writeLeaves(t.Context(), seg.batch, evmstate.Leaves{
		Keys: [][]byte{common.Hash{0x11}.Bytes()},
		Vals: [][]byte{{1}},
	}))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, tr.hashSegment(ctx, seg), context.Canceled)
	// segmentFinished must surface it rather than advance to the next segment.
	require.ErrorIs(t, tr.segmentFinished(ctx, 0), context.Canceled)
	require.Zero(t, tr.segmentToHashNext, "a failed hash must not advance the cursor")
}
