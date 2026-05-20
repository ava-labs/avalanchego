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
// returns the committed root with keys and values sorted ascending.
func FillTrie(t *testing.T, trieDB *triedb.Database, numKeys int) (common.Hash, [][]byte, [][]byte) {
	t.Helper()
	tr, err := trie.New(trie.TrieID(types.EmptyRootHash), trieDB)
	require.NoError(t, err)

	keys := make([][]byte, numKeys)
	vals := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		key := make([]byte, common.HashLength)
		binary.BigEndian.PutUint64(key, uint64(i+1))
		val := make([]byte, common.HashLength)
		binary.BigEndian.PutUint64(val, uint64(i+1)*1000)
		tr.MustUpdate(key, val)
		keys[i] = key
		vals[i] = val
	}

	root, nodes, err := tr.Commit(false)
	require.NoError(t, err)
	require.NoError(t, trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil))
	require.NoError(t, trieDB.Commit(root, false))

	// Sort to match the responder's iteration order.
	pairs := make([]struct{ k, v []byte }, numKeys)
	for i := range keys {
		pairs[i] = struct{ k, v []byte }{keys[i], vals[i]}
	}
	slices.SortFunc(pairs, func(a, b struct{ k, v []byte }) int { return bytes.Compare(a.k, b.k) })
	for i := range pairs {
		keys[i], vals[i] = pairs[i].k, pairs[i].v
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
