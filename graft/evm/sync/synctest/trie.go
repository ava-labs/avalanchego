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
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func NewTrieDB() *triedb.Database {
	return triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
}

func NewTrieDBWithDisk() (*triedb.Database, ethdb.Database) {
	db := rawdb.NewMemoryDatabase()
	return triedb.NewDatabase(db, nil), db
}

// FillTrie writes numKeys deterministic 32-byte pairs, returning the root and the
// pairs sorted ascending. Keys are unhashed, so they cluster low in the key space.
func FillTrie(t *testing.T, trieDB *triedb.Database, numKeys int) (common.Hash, [][]byte, [][]byte) {
	t.Helper()
	return fill(t, trieDB, numKeys, sequentialKey, func(i int) []byte {
		val := make([]byte, common.HashLength)
		binary.BigEndian.PutUint64(val, uint64(i+1)*1000)
		return val
	})
}

// FillTrieDistributed is [FillTrie] with hashed keys and 8-byte values, so every
// 2-byte prefix segment has data.
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

func sequentialKey(i int) []byte {
	key := make([]byte, common.HashLength)
	binary.BigEndian.PutUint64(key, uint64(i+1))
	return key
}

func hashedKey(i int) []byte {
	return HashedKey(uint64(i + 1))
}

// fill writes n pairs from keyOf and valueOf, returning the root and the pairs
// sorted ascending to match the responder's iteration order.
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

// FillAccounts adds [numAccounts] randomly generated accounts to the secure trie at [root] and commits it to [trieDB].
// [onAccount] is called if non-nil so the caller can modify the account before it is stored in the trie.
// If the trie in the callback is used (i.e. tr.Hash() doesn't return the empty root), the account's storage root will be updated to match.
// Returns the new trie root and a map of funded keys to StateAccount structs.
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
			// If the storage trie was used, update the account's storage root and pass nodes to TrieDB.
			if nodes != nil {
				require.NoError(t, mergedSet.Merge(nodes))
				acc.Root = root
			}
		}

		require.NoError(t, tr.UpdateAccount(key.Address, &acc))
		accounts[key] = &acc
	}

	newRoot, nodes, err := tr.Commit(true)
	require.NoError(t, err)
	require.NoError(t, mergedSet.Merge(nodes))
	updateOpts := stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{}) // block hashes required for Firewood
	require.NoError(t, s.TrieDB().Update(newRoot, root, 0, mergedSet, nil, updateOpts))
	require.NoError(t, s.TrieDB().Commit(newRoot, false))
	return newRoot, accounts
}

// FillAccountsWithStorageAndCode is a helper function that calls [FillAccounts] with an [onAccount] callback that randomly assigns accounts to have code and storage.
// Approximately half of accounts created will have unrunnable contracts and non-empty storage tries, and the other half will be EOAs.
func FillAccountsWithStorageAndCode(t *testing.T, r *rand.Rand, serverDB state.Database, root common.Hash, numAccounts int) (common.Hash, map[*utilstest.Key]*types.StateAccount) {
	return FillAccounts(t, r, serverDB, root, numAccounts, func(t *testing.T, _ int, addr common.Address, account types.StateAccount, storageTr state.Trie) types.StateAccount {
		if r.Intn(2) == 0 {
			codeBytes := make([]byte, 256)
			_, err := r.Read(codeBytes)
			require.NoError(t, err, "error reading random code bytes")

			codeHash := crypto.Keccak256Hash(codeBytes)
			rawdb.WriteCode(serverDB.DiskDB(), codeHash, codeBytes)
			account.CodeHash = codeHash[:]

			FillStorageForAccount(t, r, 16, addr, storageTr)
		}
		return account
	})
}

// FillStorageForAccount adds [numStorageKeys] random key-value pairs to the storage trie for [addr] in [storageTr].
func FillStorageForAccount(
	t *testing.T, r *rand.Rand, numStorageKeys int,
	addr common.Address, storageTr state.Trie,
) {
	keys, values := makeKeyValues(t, r, numStorageKeys, common.HashLength)
	for i := range numStorageKeys {
		require.NoError(t, storageTr.UpdateStorage(addr, keys[i], values[i]))
	}
}

func makeKeyValues(t *testing.T, r *rand.Rand, numKeys, keySize int) ([][]byte, [][]byte) {
	keys := make([][]byte, 0, numKeys)
	values := make([][]byte, 0, numKeys)

	// Generate key-value pairs
	for range numKeys {
		key := make([]byte, keySize)
		_, err := r.Read(key)
		require.NoError(t, err)

		value := make([]byte, r.Intn(128)+128) // min 128 bytes, max 255 bytes
		_, err = r.Read(value)
		require.NoError(t, err)

		keys = append(keys, key)
		values = append(values, value)
	}

	return keys, values
}
