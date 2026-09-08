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
// returns the committed root with keys and values sorted ascending. Keys are
// unhashed, so they cluster at the start of the key space.
func FillTrie(t *testing.T, trieDB *triedb.Database, numKeys int) (common.Hash, [][]byte, [][]byte) {
	t.Helper()
	tr, err := trie.New(trie.TrieID(types.EmptyRootHash), trieDB)
	require.NoError(t, err)

	type row struct{ key, val []byte }
	rows := make([]row, numKeys)
	for i := range numKeys {
		key := make([]byte, common.HashLength)
		binary.BigEndian.PutUint64(key, uint64(i+1))
		val := make([]byte, common.HashLength)
		binary.BigEndian.PutUint64(val, uint64(i+1)*1000)
		tr.MustUpdate(key, val)
		rows[i] = row{key, val}
	}

	root, nodes, err := tr.Commit(false)
	require.NoError(t, err)
	require.NoError(t, trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil))
	require.NoError(t, trieDB.Commit(root, false))

	slices.SortFunc(rows, func(a, b row) int { return bytes.Compare(a.key, b.key) })
	keys := make([][]byte, numKeys)
	vals := make([][]byte, numKeys)
	for i, r := range rows {
		keys[i], vals[i] = r.key, r.val
	}
	return root, keys, vals
}
