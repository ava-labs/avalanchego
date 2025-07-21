// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesynctest

import (
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/subnet-evm/utils/utilstest"
	"github.com/holiman/uint256"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/assert"
)

// GenerateTrie creates a trie with [numKeys] key-value pairs inside of [trieDB].
// Returns the root of the generated trie, the slice of keys inserted into the trie in lexicographical
// order, and the slice of corresponding values.
// GenerateTrie reads from [rand] and the caller should call rand.Seed(n) for deterministic results
func GenerateTrie(t *testing.T, trieDB *triedb.Database, numKeys int, keySize int) (common.Hash, [][]byte, [][]byte) {
	if keySize < wrappers.LongLen+1 {
		t.Fatal("key size must be at least 9 bytes (8 bytes for uint64 and 1 random byte)")
	}
	return FillTrie(t, 0, numKeys, keySize, trieDB, types.EmptyRootHash)
}

// FillTrie fills a given trie with [numKeys] number of keys, each of size [keySize]
// returns inserted keys and values
// FillTrie reads from [rand] and the caller should call rand.Seed(n) for deterministic results
func FillTrie(t *testing.T, start, numKeys int, keySize int, trieDB *triedb.Database, root common.Hash) (common.Hash, [][]byte, [][]byte) {
	testTrie, err := trie.New(trie.TrieID(root), trieDB)
	if err != nil {
		t.Fatalf("error creating trie: %v", err)
	}

	keys := make([][]byte, 0, numKeys)
	values := make([][]byte, 0, numKeys)

	// Generate key-value pairs
	for i := start; i < numKeys; i++ {
		key := make([]byte, keySize)
		binary.BigEndian.PutUint64(key[:wrappers.LongLen], uint64(i+1))
		_, err := rand.Read(key[wrappers.LongLen:])
		assert.NoError(t, err)

		value := make([]byte, rand.Intn(128)+128) // min 128 bytes, max 256 bytes
		_, err = rand.Read(value)
		assert.NoError(t, err)

		testTrie.MustUpdate(key, value)

		keys = append(keys, key)
		values = append(values, value)
	}

	// Commit the root to [trieDB]
	nextRoot, nodes, err := testTrie.Commit(false)
	assert.NoError(t, err)
	err = trieDB.Update(nextRoot, root, 0, trienode.NewWithNodeSet(nodes), nil)
	assert.NoError(t, err)
	err = trieDB.Commit(nextRoot, false)
	assert.NoError(t, err)

	return nextRoot, keys, values
}

// AssertTrieConsistency ensures given trieDB [a] and [b] both have the same
// non-empty trie at [root]. (all key/value pairs must be equal)
func AssertTrieConsistency(t testing.TB, root common.Hash, a, b *triedb.Database, onLeaf func(key, val []byte) error) {
	trieA, err := trie.New(trie.TrieID(root), a)
	if err != nil {
		t.Fatalf("error creating trieA, root=%s, err=%v", root, err)
	}
	trieB, err := trie.New(trie.TrieID(root), b)
	if err != nil {
		t.Fatalf("error creating trieB, root=%s, err=%v", root, err)
	}

	nodeItA, err := trieA.NodeIterator(nil)
	if err != nil {
		t.Fatalf("error creating node iterator for trieA, root=%s, err=%v", root, err)
	}
	nodeItB, err := trieB.NodeIterator(nil)
	if err != nil {
		t.Fatalf("error creating node iterator for trieB, root=%s, err=%v", root, err)
	}
	itA := trie.NewIterator(nodeItA)
	itB := trie.NewIterator(nodeItB)
	count := 0
	for itA.Next() && itB.Next() {
		count++
		assert.Equal(t, itA.Key, itB.Key)
		assert.Equal(t, itA.Value, itB.Value)
		if onLeaf != nil {
			if err := onLeaf(itA.Key, itA.Value); err != nil {
				t.Fatalf("error in onLeaf callback: %v", err)
			}
		}
	}
	assert.NoError(t, itA.Err)
	assert.NoError(t, itB.Err)
	assert.False(t, itA.Next())
	assert.False(t, itB.Next())
	assert.Greater(t, count, 0)
}

// CorruptTrie deletes every [n]th trie node from the trie given by [tr] from the underlying [db].
// Assumes [tr] can be iterated without issue.
func CorruptTrie(t *testing.T, diskdb ethdb.Batcher, tr *trie.Trie, n int) {
	// Delete some trie nodes
	batch := diskdb.NewBatch()
	nodeIt, err := tr.NodeIterator(nil)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for nodeIt.Next(true) {
		count++
		if count%n == 0 && nodeIt.Hash() != (common.Hash{}) {
			if err := batch.Delete(nodeIt.Hash().Bytes()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := nodeIt.Error(); err != nil {
		t.Fatal(err)
	}

	if err := batch.Write(); err != nil {
		t.Fatal(err)
	}
}

// FillAccounts adds [numAccounts] randomly generated accounts to the secure trie at [root] and commits it to [trieDB].
// [onAccount] is called if non-nil (so the caller can modify the account before it is stored in the secure trie).
// returns the new trie root and a map of funded keys to StateAccount structs.
func FillAccounts(
	t *testing.T, trieDB *triedb.Database, root common.Hash, numAccounts int,
	onAccount func(*testing.T, int, types.StateAccount) types.StateAccount,
) (common.Hash, map[*utilstest.Key]*types.StateAccount) {
	var (
		minBalance  = uint256.NewInt(3000000000000000000)
		randBalance = uint256.NewInt(1000000000000000000)
		maxNonce    = 10
		accounts    = make(map[*utilstest.Key]*types.StateAccount, numAccounts)
	)

	tr, err := trie.NewStateTrie(trie.TrieID(root), trieDB)
	if err != nil {
		t.Fatalf("error opening trie: %v", err)
	}

	for i := 0; i < numAccounts; i++ {
		acc := types.StateAccount{
			Nonce:    uint64(rand.Intn(maxNonce)),
			Balance:  new(uint256.Int).Add(minBalance, randBalance),
			CodeHash: types.EmptyCodeHash[:],
			Root:     types.EmptyRootHash,
		}
		if onAccount != nil {
			acc = onAccount(t, i, acc)
		}

		accBytes, err := rlp.EncodeToBytes(&acc)
		if err != nil {
			t.Fatalf("failed to rlp encode account: %v", err)
		}

		key := utilstest.NewKey(t)
		tr.MustUpdate(key.Address[:], accBytes)
		accounts[key] = &acc
	}

	newRoot, nodes, err := tr.Commit(false)
	if err != nil {
		t.Fatalf("error committing trie: %v", err)
	}
	if err := trieDB.Update(newRoot, root, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
		t.Fatalf("error updating trieDB: %v", err)
	}
	if err := trieDB.Commit(newRoot, false); err != nil {
		t.Fatalf("error committing trieDB: %v", err)
	}
	return newRoot, accounts
}
