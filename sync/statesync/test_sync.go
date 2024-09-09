// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/sync/syncutils"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

// assertDBConsistency checks [serverTrieDB] and [clientTrieDB] have the same EVM state trie at [root],
// and that [clientTrieDB.DiskDB] has corresponding account & snapshot values.
// Also verifies any code referenced by the EVM state is present in [clientTrieDB] and the hash is correct.
func assertDBConsistency(t testing.TB, root common.Hash, clientDB ethdb.Database, serverTrieDB, clientTrieDB *triedb.Database) {
	numSnapshotAccounts := 0
	accountIt := rawdb.IterateAccountSnapshots(clientDB)
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

	syncutils.AssertTrieConsistency(t, root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
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
		syncutils.AssertTrieConsistency(t, acc.Root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
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

func fillAccountsWithStorage(t *testing.T, serverDB ethdb.Database, serverTrieDB *triedb.Database, root common.Hash, numAccounts int) common.Hash {
	newRoot, _ := syncutils.FillAccounts(t, serverTrieDB, root, numAccounts, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		codeBytes := make([]byte, 256)
		_, err := rand.Read(codeBytes)
		if err != nil {
			t.Fatalf("error reading random code bytes: %v", err)
		}

		codeHash := crypto.Keccak256Hash(codeBytes)
		rawdb.WriteCode(serverDB, codeHash, codeBytes)
		account.CodeHash = codeHash[:]

		// now create state trie
		numKeys := 16
		account.Root, _, _ = syncutils.GenerateTrie(t, serverTrieDB, numKeys, common.HashLength)
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
	t *testing.T, trieDB *triedb.Database, root common.Hash, numAccounts int, numOverlappingStorageRoots int,
) (common.Hash, map[*keystore.Key]*types.StateAccount) {
	storageRoots := make([]common.Hash, 0, numOverlappingStorageRoots)
	for i := 0; i < numOverlappingStorageRoots; i++ {
		storageRoot, _, _ := syncutils.GenerateTrie(t, trieDB, 16, common.HashLength)
		storageRoots = append(storageRoots, storageRoot)
	}
	storageRootIndex := 0
	return syncutils.FillAccounts(t, trieDB, root, numAccounts, func(t *testing.T, i int, account types.StateAccount) types.StateAccount {
		switch i % 3 {
		case 0: // unmodified account
		case 1: // account with overlapping storage root
			account.Root = storageRoots[storageRootIndex%numOverlappingStorageRoots]
			storageRootIndex++
		case 2: // account with unique storage root
			account.Root, _, _ = syncutils.GenerateTrie(t, trieDB, 16, common.HashLength)
		}

		return account
	})
}
