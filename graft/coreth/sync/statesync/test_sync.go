// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	cryptoRand "crypto/rand"
	"math/big"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/accounts/keystore"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

// assertDBConsistency checks [serverTrieDB] and [clientTrieDB] have the same EVM state trie at [root],
// and that [clientTrieDB.DiskDB] has corresponding account & snapshot values.
// Also verifies any code referenced by the EVM state is present in [clientTrieDB] and the hash is correct.
func assertDBConsistency(t testing.TB, root common.Hash, serverTrieDB, clientTrieDB *trie.Database) {
	clientDB := clientTrieDB.DiskDB()
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

	trie.AssertTrieConsistency(t, root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
		trieAccountLeaves++
		accHash := common.BytesToHash(key)
		var acc types.StateAccount
		if err := rlp.DecodeBytes(val, &acc); err != nil {
			return err
		}
		// check snapshot consistency
		snapshotVal := rawdb.ReadAccountSnapshot(clientTrieDB.DiskDB(), accHash)
		expectedSnapshotVal := snapshot.SlimAccountRLP(acc.Nonce, acc.Balance, acc.Root, acc.CodeHash, acc.IsMultiCoin)
		assert.Equal(t, expectedSnapshotVal, snapshotVal)

		// check code consistency
		if !bytes.Equal(acc.CodeHash, types.EmptyCodeHash[:]) {
			codeHash := common.BytesToHash(acc.CodeHash)
			code := rawdb.ReadCode(clientTrieDB.DiskDB(), codeHash)
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
		trie.AssertTrieConsistency(t, acc.Root, serverTrieDB, clientTrieDB, func(key, val []byte) error {
			storageTrieLeavesCount++
			snapshotVal := rawdb.ReadStorageSnapshot(clientTrieDB.DiskDB(), accHash, common.BytesToHash(key))
			assert.Equal(t, val, snapshotVal)
			return nil
		})

		assert.Equal(t, storageTrieLeavesCount, snapshotStorageKeysCount)
		return nil
	})

	// Check that the number of accounts in the snapshot matches the number of leaves in the accounts trie
	assert.Equal(t, trieAccountLeaves, numSnapshotAccounts)
}

func fillAccountsWithStorage(t *testing.T, serverTrieDB *trie.Database, root common.Hash, numAccounts int) common.Hash {
	newRoot, _ := FillAccounts(t, serverTrieDB, root, numAccounts, func(t *testing.T, index int, account types.StateAccount) types.StateAccount {
		codeBytes := make([]byte, 256)
		_, err := rand.Read(codeBytes)
		if err != nil {
			t.Fatalf("error reading random code bytes: %v", err)
		}

		codeHash := crypto.Keccak256Hash(codeBytes)
		rawdb.WriteCode(serverTrieDB.DiskDB(), codeHash, codeBytes)
		account.CodeHash = codeHash[:]

		// now create state trie
		numKeys := 16
		account.Root, _, _ = trie.GenerateTrie(t, serverTrieDB, numKeys, wrappers.LongLen+1)
		return account
	})
	return newRoot
}

// FillAccounts adds [numAccounts] randomly generated accounts to the secure trie at [root] and commits it to [trieDB].
// [onAccount] is called if non-nil (so the caller can modify the account before it is stored in the secure trie).
// returns the new trie root and a map of funded keys to StateAccount structs.
func FillAccounts(
	t *testing.T, trieDB *trie.Database, root common.Hash, numAccounts int,
	onAccount func(*testing.T, int, types.StateAccount) types.StateAccount,
) (common.Hash, map[*keystore.Key]*types.StateAccount) {
	var (
		minBalance  = big.NewInt(3000000000000000000)
		randBalance = big.NewInt(1000000000000000000)
		maxNonce    = 10
		accounts    = make(map[*keystore.Key]*types.StateAccount, numAccounts)
	)

	tr, err := trie.NewSecure(root, trieDB)
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

		accBytes, err := rlp.EncodeToBytes(acc)
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

	newRoot, _, err := tr.Commit(nil)
	if err != nil {
		t.Fatalf("error committing trie: %v", err)
	}
	if err := trieDB.Commit(newRoot, false, nil); err != nil {
		t.Fatalf("error committing trieDB: %v", err)
	}
	return newRoot, accounts
}

// FillAccountsWithOverlappingStorage adds [numAccounts] randomly generated accounts to the secure trie at [root]
// and commits it to [trieDB]. For each 3 accounts created:
// - One does not have a storage trie,
// - One has a storage trie shared with other accounts (total number of shared storage tries [numOverlappingStorageRoots]),
// - One has a uniquely generated storage trie,
// returns the new trie root and a map of funded keys to StateAccount structs.
func FillAccountsWithOverlappingStorage(
	t *testing.T, trieDB *trie.Database, root common.Hash, numAccounts int, numOverlappingStorageRoots int,
) (common.Hash, map[*keystore.Key]*types.StateAccount) {
	storageRoots := make([]common.Hash, 0, numOverlappingStorageRoots)
	for i := 0; i < numOverlappingStorageRoots; i++ {
		storageRoot, _, _ := trie.GenerateTrie(t, trieDB, 16, common.HashLength)
		storageRoots = append(storageRoots, storageRoot)
	}
	storageRootIndex := 0
	return FillAccounts(t, trieDB, root, numAccounts, func(t *testing.T, i int, account types.StateAccount) types.StateAccount {
		switch i % 3 {
		case 0: // unmodified account
		case 1: // account with overlapping storage root
			account.Root = storageRoots[storageRootIndex%numOverlappingStorageRoots]
			storageRootIndex++
		case 2: // account with unique storage root
			account.Root, _, _ = trie.GenerateTrie(t, trieDB, 16, common.HashLength)
		}

		return account
	})
}
