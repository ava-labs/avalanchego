// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// NewTrieDB returns an in-memory [triedb.Database].
func NewTrieDB() *triedb.Database {
	return triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
}

// NewTrieDBWithDisk returns an in-memory [triedb.Database] and its
// backing [ethdb.Database].
func NewTrieDBWithDisk() (*triedb.Database, ethdb.Database) {
	db := rawdb.NewMemoryDatabase()
	return triedb.NewDatabase(db, nil), db
}

// FillTrie writes numKeys deterministic 32-byte pairs into trieDB and
// returns the committed root with keys and values sorted ascending. Keys are
// unhashed, so they cluster at the start of the key space.
func FillTrie(t *testing.T, trieDB *triedb.Database, numKeys int) (common.Hash, [][]byte, [][]byte) {
	t.Helper()
	return fill(t, trieDB, numKeys, sequentialKey, func(i int) []byte {
		val := make([]byte, common.HashLength)
		binary.BigEndian.PutUint64(val, uint64(i+1)*1000)
		return val
	})
}

// FillTrieDistributed writes numKeys pairs whose keys are hashed, so they spread
// across the key space and segmentation by 2-byte prefix has data in every range.
// Values are 8 bytes.
func FillTrieDistributed(t *testing.T, trieDB *triedb.Database, numKeys int) (common.Hash, [][]byte, [][]byte) {
	t.Helper()
	return fill(t, trieDB, numKeys, hashedKey, func(i int) []byte {
		val := make([]byte, 8)
		binary.BigEndian.PutUint64(val, uint64(i+1)*7)
		return val
	})
}

// FillAccountTrieDistributed is [FillTrieDistributed] with full-RLP account values,
// for reconstructing an account trie.
func FillAccountTrieDistributed(t *testing.T, trieDB *triedb.Database, numAccounts int) (common.Hash, [][]byte, [][]byte) {
	t.Helper()
	return fill(t, trieDB, numAccounts, hashedKey, func(i int) []byte {
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

// sequentialKey returns the unhashed 32-byte trie key for the i-th entry.
func sequentialKey(i int) []byte {
	key := make([]byte, common.HashLength)
	binary.BigEndian.PutUint64(key, uint64(i+1))
	return key
}

// hashedKey returns the hashed 32-byte trie key for the i-th entry.
func hashedKey(i int) []byte { return HashedKey(uint64(i + 1)) }

// fill writes n pairs built by keyOf and valueOf into trieDB and returns the
// committed root with keys and values sorted ascending, matching the responder's
// iteration order.
func fill(t *testing.T, trieDB *triedb.Database, n int, keyOf, valueOf func(i int) []byte) (common.Hash, [][]byte, [][]byte) {
	t.Helper()
	tr, err := trie.New(trie.TrieID(types.EmptyRootHash), trieDB)
	require.NoError(t, err)

	type row struct{ key, val []byte }
	rows := make([]row, n)
	for i := range n {
		key, val := keyOf(i), valueOf(i)
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

// CorruptTrie deletes every nth node of tr from diskdb to exercise
// proof-generation error paths.
func CorruptTrie(t *testing.T, diskdb ethdb.Batcher, tr *trie.Trie, n int) {
	t.Helper()
	batch := diskdb.NewBatch()
	nodeIt, err := tr.NodeIterator(nil)
	require.NoError(t, err)
	count := 0
	for nodeIt.Next(true) {
		count++
		if count%n == 0 && nodeIt.Hash() != (common.Hash{}) {
			require.NoError(t, batch.Delete(nodeIt.Hash().Bytes()))
		}
	}
	require.NoError(t, nodeIt.Error())
	require.NoError(t, batch.Write())
}
