// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"bytes"
	"encoding/binary"
	"math/rand"
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

	"github.com/ava-labs/avalanchego/utils/wrappers"
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
func hashedKey(i int) []byte {
	return HashedKey(uint64(i + 1))
}

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

// GenerateIndependentTrie creates a trie with [numKeys] random key-value pairs inside of [trieDB].
// Returns the root of the generated trie, the slice of keys inserted into the trie in lexicographical
// order, and the slice of corresponding values.
//
// This is safe for use with HashDB, intended for use creating a storage trie independent of an account,
// or for an atomic trie.
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

	keys := make([][]byte, 0, numKeys)
	values := make([][]byte, 0, numKeys)

	// Generate key-value pairs
	for i := start; i < numKeys; i++ {
		key := make([]byte, keySize)
		binary.BigEndian.PutUint64(key[:wrappers.LongLen], uint64(i+1))
		_, err := r.Read(key[wrappers.LongLen:])
		require.NoError(t, err)
		value := make([]byte, r.Intn(128)+128) // min 128 bytes, max 255 bytes
		_, err = r.Read(value)
		require.NoError(t, err)

		testTrie.MustUpdate(key, value)

		keys = append(keys, key)
		values = append(values, value)
	}

	// Commit the root to [trieDB]
	nextRoot, nodes, err := testTrie.Commit(false)
	require.NoError(t, err)
	require.NoError(t, trieDB.Update(nextRoot, root, 0, trienode.NewWithNodeSet(nodes), nil))
	require.NoError(t, trieDB.Commit(nextRoot, false))

	return nextRoot, keys, values
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
