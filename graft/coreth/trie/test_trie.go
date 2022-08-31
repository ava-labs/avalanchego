// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trie

import (
	cryptoRand "crypto/rand"
	"encoding/binary"
	"math/big"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/core/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

// GenerateTrie creates a trie with [numKeys] key-value pairs inside of [trieDB].
// Returns the root of the generated trie, the slice of keys inserted into the trie in lexicographical
// order, and the slice of corresponding values.
// GenerateTrie reads from [rand] and the caller should call rand.Seed(n) for deterministic results
func GenerateTrie(t *testing.T, trieDB *Database, numKeys int, keySize int) (common.Hash, [][]byte, [][]byte) {
	if keySize < wrappers.LongLen+1 {
		t.Fatal("key size must be at least 9 bytes (8 bytes for uint64 and 1 random byte)")
	}
	testTrie := NewEmpty(trieDB)

	keys, values := FillTrie(t, numKeys, keySize, testTrie)

	// Commit the root to [trieDB]
	root, nodes, err := testTrie.Commit(false)
	assert.NoError(t, err)
	err = trieDB.Update(NewWithNodeSet(nodes))
	assert.NoError(t, err)
	err = trieDB.Commit(root, false, nil)
	assert.NoError(t, err)

	return root, keys, values
}

// FillTrie fills a given trie with [numKeys] number of keys, each of size [keySize]
// returns inserted keys and values
// FillTrie reads from [rand] and the caller should call rand.Seed(n) for deterministic results
func FillTrie(t *testing.T, numKeys int, keySize int, testTrie *Trie) ([][]byte, [][]byte) {
	keys := make([][]byte, 0, numKeys)
	values := make([][]byte, 0, numKeys)

	// Generate key-value pairs
	for i := 0; i < numKeys; i++ {
		key := make([]byte, keySize)
		binary.BigEndian.PutUint64(key[:wrappers.LongLen], uint64(i+1))
		_, err := rand.Read(key[wrappers.LongLen:])
		assert.NoError(t, err)

		value := make([]byte, rand.Intn(128)+128) // min 128 bytes, max 256 bytes
		_, err = rand.Read(value)
		assert.NoError(t, err)

		if err = testTrie.TryUpdate(key, value); err != nil {
			t.Fatal("error updating trie", err)
		}

		keys = append(keys, key)
		values = append(values, value)
	}
	return keys, values
}

// AssertTrieConsistency ensures given trieDB [a] and [b] both have the same
// non-empty trie at [root]. (all key/value pairs must be equal)
func AssertTrieConsistency(t testing.TB, root common.Hash, a, b *Database, onLeaf func(key, val []byte) error) {
	trieA, err := New(common.Hash{}, root, a)
	if err != nil {
		t.Fatalf("error creating trieA, root=%s, err=%v", root, err)
	}
	trieB, err := New(common.Hash{}, root, b)
	if err != nil {
		t.Fatalf("error creating trieB, root=%s, err=%v", root, err)
	}

	itA := NewIterator(trieA.NodeIterator(nil))
	itB := NewIterator(trieB.NodeIterator(nil))
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

// CorruptTrie deletes every [n]th trie node from the trie given by [root] from the trieDB.
// Assumes that the trie given by root can be iterated without issue.
func CorruptTrie(t *testing.T, trieDB *Database, root common.Hash, n int) {
	batch := trieDB.DiskDB().NewBatch()
	// next delete some trie nodes
	tr, err := New(common.Hash{}, root, trieDB)
	if err != nil {
		t.Fatal(err)
	}

	nodeIt := tr.NodeIterator(nil)
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
	t *testing.T, trieDB *Database, root common.Hash, numAccounts int,
	onAccount func(*testing.T, int, types.StateAccount) types.StateAccount,
) (common.Hash, map[*keystore.Key]*types.StateAccount) {
	var (
		minBalance  = big.NewInt(3000000000000000000)
		randBalance = big.NewInt(1000000000000000000)
		maxNonce    = 10
		accounts    = make(map[*keystore.Key]*types.StateAccount, numAccounts)
	)

	tr, err := NewStateTrie(common.Hash{}, root, trieDB)
	if err != nil {
		t.Fatalf("error opening trie: %v", err)
	}

	for i := 0; i < numAccounts; i++ {
		acc := types.StateAccount{
			Nonce:    uint64(rand.Intn(maxNonce)),
			Balance:  new(big.Int).Add(minBalance, randBalance),
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

		key, err := keystore.NewKey(cryptoRand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err = tr.TryUpdate(key.Address[:], accBytes); err != nil {
			t.Fatalf("error updating trie with account, address=%s, err=%v", key.Address, err)
		}
		accounts[key] = &acc
	}

	newRoot, nodes, err := tr.Commit(false)
	if err != nil {
		t.Fatalf("error committing trie: %v", err)
	}
	if err := trieDB.Update(NewWithNodeSet(nodes)); err != nil {
		t.Fatalf("error updating trieDB: %v", err)
	}
	if err := trieDB.Commit(newRoot, false, nil); err != nil {
		t.Fatalf("error committing trieDB: %v", err)
	}
	return newRoot, accounts
}
