// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// FillAccountTrie writes numAccounts deterministic accounts into trieDB. It
// returns the root, sorted keys and full-RLP leaves, and a matching
// [StaticSnapshot] of slim accounts.
func FillAccountTrie(t *testing.T, trieDB *triedb.Database, numAccounts int) (common.Hash, [][]byte, [][]byte, *StaticSnapshot) {
	t.Helper()
	tr, err := trie.New(trie.TrieID(types.EmptyRootHash), trieDB)
	require.NoError(t, err)

	type row struct{ key, full, slim []byte }
	rows := make([]row, numAccounts)
	for i := range numAccounts {
		key, full, slim := accountLeaf(t, i, uint64(i+1))
		tr.MustUpdate(key, full)
		rows[i] = row{key, full, slim}
	}

	root, nodes, err := tr.Commit(false)
	require.NoError(t, err)
	require.NoError(t, trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil))
	require.NoError(t, trieDB.Commit(root, false))

	slices.SortFunc(rows, func(a, b row) int { return bytes.Compare(a.key, b.key) })
	keys := make([][]byte, numAccounts)
	vals := make([][]byte, numAccounts)
	pairs := make([]StaticPair, numAccounts)
	for i, r := range rows {
		keys[i], vals[i] = r.key, r.full
		pairs[i] = StaticPair{K: r.key, V: r.slim}
	}
	return root, keys, vals, &StaticSnapshot{Accounts: pairs}
}

// accountLeaf is the deterministic account at index i, slim and full encoded.
func accountLeaf(t *testing.T, i int, nonce uint64) (key, full, slim []byte) {
	t.Helper()
	key = make([]byte, common.HashLength)
	binary.BigEndian.PutUint64(key, uint64(i+1))
	slim = types.SlimAccountRLP(types.StateAccount{
		Nonce:    nonce,
		Balance:  uint256.NewInt(uint64(i+1) * 1000),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	})
	full, err := types.FullAccountRLP(slim)
	require.NoError(t, err)
	return key, full, slim
}

// AdvanceAccountTrie rewrites the first numAccounts accounts of from, returning
// the newer root. from stays readable.
func AdvanceAccountTrie(t *testing.T, trieDB *triedb.Database, from common.Hash, numAccounts int) common.Hash {
	t.Helper()
	tr, err := trie.New(trie.TrieID(from), trieDB)
	require.NoError(t, err)

	// Past any nonce FillAccountTrie assigns, so every account really changes.
	const nonceOffset = 1_000_000

	for i := range numAccounts {
		key, full, _ := accountLeaf(t, i, uint64(i+1)+nonceOffset)
		tr.MustUpdate(key, full)
	}

	root, nodes, err := tr.Commit(false)
	require.NoError(t, err)
	require.NoError(t, trieDB.Update(root, from, 0, trienode.NewWithNodeSet(nodes), nil))
	require.NoError(t, trieDB.Commit(root, false))
	return root
}
