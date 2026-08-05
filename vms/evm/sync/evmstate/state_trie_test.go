// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
)

// TestStateTrie_SegmentedStorageReconstruct proves a storage trie split into concurrent segments reconstructs via snapshot re-read.
func TestStateTrie_SegmentedStorageReconstruct(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	account := common.HexToHash("0xac")
	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrieDistributed(t, trieDB, 3000)

	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, RegisterHandler(logging.NoLog{}, net, trieDB, common.HashLength, nil))

	target := rawdb.NewMemoryDatabase()
	leaves := newStorageLeafStore(target, []common.Hash{account})

	tasks := make(chan task, 64)
	st, err := newStateTrie(target, root, account, leaves, stateTrieConfig{
		numSegments: numStorageTrieSegments,
		threshold:   1,
		tasks:       tasks,
		onDone:      func(context.Context) error { close(tasks); return nil },
	})
	require.NoError(t, err)
	tasks <- st.segments[0]

	require.NoError(t, newLeafFetcher(logging.NoLog{}, NewClient(net, tracker), tasks, 4).sync(ctx))

	require.Greater(t, len(st.segments), 1, "the storage trie must have split into segments")
	requireReconstructed(t, target, root, keys, vals)
	for i, k := range keys {
		require.Equal(t, vals[i], rawdb.ReadStorageSnapshot(target, account, common.BytesToHash(k)))
	}
}
