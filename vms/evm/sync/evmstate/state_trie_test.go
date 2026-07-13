// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
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
	root, keys, vals := fillDistributedStorageTrie(t, trieDB, 3000)

	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, RegisterHandler(net, logging.NoLog{}, trieDB, common.HashLength, nil))

	target := rawdb.NewMemoryDatabase()
	leaves := newStorageLeaves(target, []common.Hash{account})

	tasks := make(chan task, 64)
	st, err := newStateTrie(target, root, account, leaves, stateTrieConfig{
		numSegments: numStorageTrieSegments,
		threshold:   1,
		tasks:       tasks,
		onDone:      func(context.Context) error { close(tasks); return nil },
	})
	require.NoError(t, err)
	tasks <- st.segments[0]

	require.NoError(t, newCallbackSyncer(NewClient(net, tracker), tasks, 4).sync(ctx))

	require.Greater(t, len(st.segments), 1, "the storage trie must have split into segments")
	requireReconstructed(t, target, root, keys, vals)
	for i, k := range keys {
		require.Equal(t, vals[i], rawdb.ReadStorageSnapshot(target, account, common.BytesToHash(k)))
	}
}

// fillDistributedTrie writes n pairs whose hashed keys spread across the key space,
// so segmentation by 2-byte prefix has data in every range. Returns the root with keys and values sorted.
func fillDistributedTrie(t *testing.T, trieDB *triedb.Database, n int, valueOf func(i int) []byte) (common.Hash, [][]byte, [][]byte) {
	t.Helper()
	tr, err := trie.New(trie.TrieID(types.EmptyRootHash), trieDB)
	require.NoError(t, err)

	type row struct{ key, val []byte }
	rows := make([]row, n)
	for i := range n {
		var idx [8]byte
		binary.BigEndian.PutUint64(idx[:], uint64(i+1))
		key := crypto.Keccak256(idx[:])
		val := valueOf(i)
		tr.MustUpdate(key, val)
		rows[i] = row{key, val}
	}

	root, nodes, err := tr.Commit(false)
	require.NoError(t, err)
	require.NoError(t, trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil))
	require.NoError(t, trieDB.Commit(root, false))

	slices.SortFunc(rows, func(a, b row) int { return bytes.Compare(a.key, b.key) })
	keys := make([][]byte, n)
	vals := make([][]byte, n)
	for i, r := range rows {
		keys[i], vals[i] = r.key, r.val
	}
	return root, keys, vals
}

func fillDistributedStorageTrie(t *testing.T, trieDB *triedb.Database, numSlots int) (common.Hash, [][]byte, [][]byte) {
	return fillDistributedTrie(t, trieDB, numSlots, func(i int) []byte {
		val := make([]byte, 8)
		binary.BigEndian.PutUint64(val, uint64(i+1)*7)
		return val
	})
}

func fillDistributedAccountTrie(t *testing.T, trieDB *triedb.Database, numAccounts int) (common.Hash, [][]byte, [][]byte) {
	return fillDistributedTrie(t, trieDB, numAccounts, func(i int) []byte {
		full, err := types.FullAccountRLP(types.SlimAccountRLP(types.StateAccount{
			Nonce:    uint64(i + 1),
			Balance:  uint256.NewInt(uint64(i+1) * 1000),
			Root:     types.EmptyRootHash,
			CodeHash: types.EmptyCodeHash.Bytes(),
		}))
		require.NoError(t, err)
		return full
	})
}
