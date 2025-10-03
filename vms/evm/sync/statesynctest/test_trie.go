// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesynctest

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/utils/utilstest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// GenerateTrie creates a trie with [numKeys] random key-value pairs inside of [trieDB].
// Returns the root of the generated trie, the slice of keys inserted into the trie in lexicographical
// order, and the slice of corresponding values.
// GenerateTrie reads from [r]
func GenerateTrie(t *testing.T, r io.Reader, trieDB *triedb.Database, numKeys int, keySize int) (common.Hash, [][]byte, [][]byte) {
	require.GreaterOrEqual(t, keySize, wrappers.LongLen+1, "key size must be at least 9 bytes (8 bytes for uint64 and 1 random byte)")
	return FillTrie(t, r, 0, numKeys, keySize, trieDB, types.EmptyRootHash)
}

// FillTrie fills a given trie with [numKeys] random keys, each of size [keySize]
// returns inserted keys and values
func FillTrie(t *testing.T, r io.Reader, start, numKeys int, keySize int, trieDB *triedb.Database, root common.Hash) (common.Hash, [][]byte, [][]byte) {
	testTrie, err := trie.New(trie.TrieID(root), trieDB)
	require.NoError(t, err)

	keys := make([][]byte, 0, numKeys)
	values := make([][]byte, 0, numKeys)

	// Generate key-value pairs
	for i := start; i < numKeys; i++ {
		key := make([]byte, keySize)
		binary.BigEndian.PutUint64(key[:wrappers.LongLen], uint64(i+1))
		_, err := io.ReadFull(r, key[wrappers.LongLen:])
		require.NoError(t, err)

		nBig, err := rand.Int(rand.Reader, big.NewInt(128))
		require.NoError(t, err)
		value := make([]byte, int(nBig.Int64())+128) // min 128 bytes, max 256 bytes
		_, err = io.ReadFull(r, value)
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

// CorruptTrie deletes every [n]th trie node from the trie given by [tr] from the underlying [db].
// Assumes [tr] can be iterated without issue.
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
	require.NoError(t, err)

	for i := range numAccounts {
		// random nonce in [0, maxNonce)
		nBig, err := rand.Int(rand.Reader, big.NewInt(int64(maxNonce)))
		require.NoError(t, err)
		acc := types.StateAccount{
			Nonce:    uint64(nBig.Int64()),
			Balance:  new(uint256.Int).Add(minBalance, randBalance),
			CodeHash: types.EmptyCodeHash[:],
			Root:     types.EmptyRootHash,
		}
		if onAccount != nil {
			acc = onAccount(t, i, acc)
		}

		accBytes, err := rlp.EncodeToBytes(&acc)
		require.NoError(t, err)

		key := utilstest.NewKey(t)
		tr.MustUpdate(key.Address[:], accBytes)
		accounts[key] = &acc
	}

	newRoot, nodes, err := tr.Commit(false)
	require.NoError(t, err)
	require.NoError(t, trieDB.Update(newRoot, root, 0, trienode.NewWithNodeSet(nodes), nil))
	require.NoError(t, trieDB.Commit(newRoot, false))

	return newRoot, accounts
}
