// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesynctest

import (
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// GenerateIndependentTrie creates a trie with [numKeys] random key-value pairs inside of [trieDB].
// Returns the root of the generated trie, the slice of keys inserted into the trie in lexicographical
// order, and the slice of corresponding values.
//
// This is safe for use with HashDB.
func GenerateIndependentTrie(t *testing.T, r *rand.Rand, trieDB *triedb.Database, numKeys int, keySize int) (common.Hash, [][]byte, [][]byte) {
	require.GreaterOrEqual(t, keySize, wrappers.LongLen+1, "key size must be at least 9 bytes (8 bytes for uint64 and 1 random byte)")
	return FillIndependentTrie(t, r, 0, numKeys, keySize, trieDB, types.EmptyRootHash)
}

// FillIndependentTrie fills a given trie with [numKeys] random keys, each of size [keySize]
// returns inserted keys and values
//
// This is safe for use with HashDB.
func FillIndependentTrie(t *testing.T, r *rand.Rand, start, numKeys int, keySize int, trieDB *triedb.Database, root common.Hash) (common.Hash, [][]byte, [][]byte) {
	testTrie, err := trie.New(trie.TrieID(root), trieDB)
	require.NoError(t, err)

	keys, values := makeKeyValues(t, r, start, numKeys, keySize)
	for i := start; i < numKeys; i++ {
		testTrie.MustUpdate(keys[i], values[i])
	}

	// Commit the root to [trieDB]
	nextRoot, nodes, err := testTrie.Commit(false)
	require.NoError(t, err)
	require.NoError(t, trieDB.Update(nextRoot, root, 0, trienode.NewWithNodeSet(nodes), nil))
	require.NoError(t, trieDB.Commit(nextRoot, false))

	return nextRoot, keys, values
}

func makeKeyValues(t *testing.T, r *rand.Rand, start, numKeys, keySize int) ([][]byte, [][]byte) {
	keys := make([][]byte, 0, numKeys)
	values := make([][]byte, 0, numKeys)

	// Generate key-value pairs
	for i := start; i < numKeys; i++ {
		key := make([]byte, keySize)
		binary.BigEndian.PutUint64(key[:wrappers.LongLen], uint64(i+1))
		_, err := r.Read(key[wrappers.LongLen:])
		require.NoError(t, err)

		value := make([]byte, r.Intn(128)+128) // min 128 bytes, max 256 bytes
		_, err = r.Read(value)
		require.NoError(t, err)

		keys = append(keys, key)
		values = append(values, value)
	}

	return keys, values
}

// AssertTrieConsistency ensures given trieDB [a] and [b] both have the same
// non-empty trie at [root]. (all key/value pairs must be equal)
//
// This is only safe for HashDB or PathDB, since Firewood doesn't store trie nodes individually.
func AssertTrieConsistency(t testing.TB, root common.Hash, a, b *triedb.Database, onLeaf func(key, val []byte) error) {
	trieA, err := trie.New(trie.TrieID(root), a)
	require.NoError(t, err)
	trieB, err := trie.New(trie.TrieID(root), b)
	require.NoError(t, err)

	nodeItA, err := trieA.NodeIterator(nil)
	require.NoError(t, err)
	nodeItB, err := trieB.NodeIterator(nil)
	require.NoError(t, err)
	itA := trie.NewIterator(nodeItA)
	itB := trie.NewIterator(nodeItB)

	count := 0
	for itA.Next() && itB.Next() {
		count++
		require.Equal(t, itA.Key, itB.Key)
		require.Equal(t, itA.Value, itB.Value)
		if onLeaf != nil {
			require.NoError(t, onLeaf(itA.Key, itA.Value))
		}
	}
	require.NoError(t, itA.Err)
	require.NoError(t, itB.Err)
	require.False(t, itA.Next())
	require.False(t, itB.Next())
	require.Positive(t, count)
}

// CorruptTrie deletes every [n]th trie node from the trie given by [tr] from the underlying [db].
// Assumes [tr] can be iterated without issue.
//
// This is only safe for HashDB or PathDB, since Firewood doesn't store trie nodes individually.
func CorruptTrie(t *testing.T, diskdb ethdb.Batcher, tr *trie.Trie, n int) {
	// Delete some trie nodes
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

// FillAccounts adds [numAccounts] randomly generated accounts to the secure trie at [root] and commits it to [trieDB].
// [onAccount] is called if non-nil (so the caller can modify the account before it is stored in the secure trie).
// returns the new trie root and a map of funded keys to StateAccount structs.
func FillAccounts(
	t *testing.T, r *rand.Rand, s state.Database, root common.Hash, numAccounts int,
	onAccount func(*testing.T, int, common.Address, types.StateAccount, state.Trie) types.StateAccount,
) (common.Hash, map[*utilstest.Key]*types.StateAccount) {
	var (
		minBalance  = uint256.NewInt(3000000000000000000)
		randBalance = uint256.NewInt(1000000000000000000)
		maxNonce    = 10
		accounts    = make(map[*utilstest.Key]*types.StateAccount, numAccounts)
		mergedSet   = trienode.NewMergedNodeSet()
	)

	tr, err := s.OpenTrie(root)
	require.NoError(t, err)

	for i := 0; i < numAccounts; i++ {
		key := utilstest.NewKey(t)
		acc := types.StateAccount{
			Nonce:    uint64(r.Intn(maxNonce)),
			Balance:  new(uint256.Int).Add(minBalance, randBalance),
			CodeHash: types.EmptyCodeHash[:],
			Root:     types.EmptyRootHash,
		}
		if onAccount != nil {
			storageTr, err := s.OpenStorageTrie(root, key.Address, types.EmptyRootHash, tr)
			require.NoError(t, err)
			acc = onAccount(t, i, key.Address, acc, storageTr)
			root, nodes, err := storageTr.Commit(false)
			require.NoError(t, err)
			if root != types.EmptyRootHash {
				require.NoError(t, mergedSet.Merge(nodes))
				acc.Root = root
			}

		}

		tr.UpdateAccount(key.Address, &acc)
		accounts[key] = &acc
	}

	newRoot, nodes, err := tr.Commit(false)
	require.NoError(t, err)
	require.NoError(t, mergedSet.Merge(nodes))
	require.NoError(t, s.TrieDB().Update(newRoot, root, 0, mergedSet, nil))
	require.NoError(t, s.TrieDB().Commit(newRoot, false))
	return newRoot, accounts
}

func FillStorageForAccount(
	t *testing.T, r *rand.Rand, root common.Hash, numStorageKeys int,
	addr common.Address, storageTr state.Trie,
) {
	keys, values := makeKeyValues(t, r, 0, numStorageKeys, common.HashLength)
	for i := range numStorageKeys {
		storageTr.UpdateStorage(addr, keys[i], values[i])
	}
}
