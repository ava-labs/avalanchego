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
)

// TestStateTrie_SegmentedStorageReconstruct proves a storage trie split into concurrent segments reconstructs via snapshot re-read.
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
