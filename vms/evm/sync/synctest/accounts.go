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
	for i := 0; i < numAccounts; i++ {
		key := make([]byte, common.HashLength)
		binary.BigEndian.PutUint64(key, uint64(i+1))
		slim := types.SlimAccountRLP(types.StateAccount{
			Nonce:    uint64(i + 1),
			Balance:  uint256.NewInt(uint64(i+1) * 1000),
			Root:     types.EmptyRootHash,
			CodeHash: types.EmptyCodeHash.Bytes(),
		})
		full, err := types.FullAccountRLP(slim)
		require.NoError(t, err)
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
