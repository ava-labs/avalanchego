// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
)

// FillAccountsWithOverlappingStorage cycles accounts through no storage, a shared
// storage root, and a unique one. HashDB only, path-based DBs do not share tries.
func FillAccountsWithOverlappingStorage(
	t *testing.T, r *rand.Rand, s state.Database, root common.Hash, numAccounts int, numOverlappingStorageRoots int,
) (common.Hash, map[*utilstest.Key]*types.StateAccount) {
	storageRoots := make([]common.Hash, 0, numOverlappingStorageRoots)
	for i := 0; i < numOverlappingStorageRoots; i++ {
		storageRoot, _, _ := GenerateIndependentTrie(t, r, s.TrieDB(), 16, common.HashLength)
		storageRoots = append(storageRoots, storageRoot)
	}
	storageRootIndex := 0
	return FillAccounts(t, r, s, root, numAccounts, func(t *testing.T, i int, addr common.Address, account types.StateAccount, storageTr state.Trie) types.StateAccount {
		switch i % 3 {
		case 0: // unmodified account
		case 1: // account with overlapping storage root
			account.Root = storageRoots[storageRootIndex%numOverlappingStorageRoots]
			storageRootIndex++
		case 2: // account with unique storage root
			FillStorageForAccount(t, r, 16, addr, storageTr)
		}

		return account
	})
}

// FillAccounts commits numAccounts random accounts onto root. onAccount may edit
// each one, and writing to the storage trie it receives sets that account's root.
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

// FillAccountsWithStorageAndCode gives roughly half the accounts code and storage,
// the rest are EOAs.
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

// FillStorageForAccount writes numStorageKeys random pairs into addr's storage trie.
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
