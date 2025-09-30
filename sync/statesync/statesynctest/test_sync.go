// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesynctest

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/subnet-evm/utils/utilstest"
)

// AssertDBConsistency checks [serverTrieDB] and [clientTrieDB] have the same EVM state trie at [root],
// and that [clientTrieDB.DiskDB] has corresponding account & snapshot values.
// Also verifies any code referenced by the EVM state is present in [clientTrieDB] and the hash is correct.
func AssertDBConsistency(t testing.TB, root common.Hash, clientDB ethdb.Database, serverTrieDB, clientTrieDB *triedb.Database) {
	numSnapshotAccounts := 0
	accountIt := customrawdb.IterateAccountSnapshots(clientDB)
	defer accountIt.Release()
	for accountIt.Next() {
		if !bytes.HasPrefix(accountIt.Key(), rawdb.SnapshotAccountPrefix) || len(accountIt.Key()) != len(rawdb.SnapshotAccountPrefix)+common.HashLength {
			continue
		}
		numSnapshotAccounts++
	}
	if err := accountIt.Error(); err != nil {
		t.Fatal(err)
	}
	trieAccountLeaves := 0

	AssertTrieConsistency(t, root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
		trieAccountLeaves++
		accHash := common.BytesToHash(key)
		var acc types.StateAccount
		if err := rlp.DecodeBytes(val, &acc); err != nil {
			return err
		}
		// check snapshot consistency
		snapshotVal := rawdb.ReadAccountSnapshot(clientDB, accHash)
		expectedSnapshotVal := types.SlimAccountRLP(acc)
		assert.Equal(t, expectedSnapshotVal, snapshotVal)

		// check code consistency
		if !bytes.Equal(acc.CodeHash, types.EmptyCodeHash[:]) {
			codeHash := common.BytesToHash(acc.CodeHash)
			code := rawdb.ReadCode(clientDB, codeHash)
			actualHash := crypto.Keccak256Hash(code)
			assert.NotZero(t, len(code))
			assert.Equal(t, codeHash, actualHash)
		}
		if acc.Root == types.EmptyRootHash {
			return nil
		}

		storageIt := rawdb.IterateStorageSnapshots(clientDB, accHash)
		defer storageIt.Release()

		snapshotStorageKeysCount := 0
		for storageIt.Next() {
			snapshotStorageKeysCount++
		}

		storageTrieLeavesCount := 0

		// check storage trie and storage snapshot consistency
		AssertTrieConsistency(t, acc.Root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
			storageTrieLeavesCount++
			snapshotVal := rawdb.ReadStorageSnapshot(clientDB, accHash, common.BytesToHash(key))
			assert.Equal(t, val, snapshotVal)
			return nil
		})

		assert.Equal(t, storageTrieLeavesCount, snapshotStorageKeysCount)
		return nil
	})

	// Check that the number of accounts in the snapshot matches the number of leaves in the accounts trie
	assert.Equal(t, trieAccountLeaves, numSnapshotAccounts)
}

func FillAccountsWithStorage(t *testing.T, r *rand.Rand, serverDB ethdb.Database, serverTrieDB *triedb.Database, root common.Hash, numAccounts int) common.Hash {
	newRoot, _ := FillAccounts(t, r, serverTrieDB, root, numAccounts, func(t *testing.T, _ int, account types.StateAccount) types.StateAccount {
		codeBytes := make([]byte, 256)
		_, err := r.Read(codeBytes)
		if err != nil {
			t.Fatalf("error reading random code bytes: %v", err)
		}

		codeHash := crypto.Keccak256Hash(codeBytes)
		rawdb.WriteCode(serverDB, codeHash, codeBytes)
		account.CodeHash = codeHash[:]

		// now create state trie
		numKeys := 16
		account.Root, _, _ = GenerateTrie(t, r, serverTrieDB, numKeys, common.HashLength)
		return account
	})
	return newRoot
}

// FillAccountsWithOverlappingStorage adds [numAccounts] randomly generated accounts to the secure trie at [root]
// and commits it to [trieDB]. For each 3 accounts created:
// - One does not have a storage trie,
// - One has a storage trie shared with other accounts (total number of shared storage tries [numOverlappingStorageRoots]),
// - One has a uniquely generated storage trie,
// returns the new trie root and a map of funded keys to StateAccount structs.
func FillAccountsWithOverlappingStorage(
	t *testing.T, r *rand.Rand, trieDB *triedb.Database, root common.Hash, numAccounts int, numOverlappingStorageRoots int,
) (common.Hash, map[*utilstest.Key]*types.StateAccount) {
	storageRoots := make([]common.Hash, 0, numOverlappingStorageRoots)
	for i := 0; i < numOverlappingStorageRoots; i++ {
		storageRoot, _, _ := GenerateTrie(t, r, trieDB, 16, common.HashLength)
		storageRoots = append(storageRoots, storageRoot)
	}
	storageRootIndex := 0
	return FillAccounts(t, r, trieDB, root, numAccounts, func(t *testing.T, i int, account types.StateAccount) types.StateAccount {
		switch i % 3 {
		case 0: // unmodified account
		case 1: // account with overlapping storage root
			account.Root = storageRoots[storageRootIndex%numOverlappingStorageRoots]
			storageRootIndex++
		case 2: // account with unique storage root
			account.Root, _, _ = GenerateTrie(t, r, trieDB, 16, common.HashLength)
		}

		return account
	})
}
