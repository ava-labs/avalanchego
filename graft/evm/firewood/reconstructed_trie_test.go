// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"testing"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// newInternalStateDB creates a state.Database backed by the given TrieDB for testing.
func newInternalStateDB(t *testing.T, db *TrieDB) state.Database {
	t.Helper()
	triedbConfig := &triedb.Config{
		DBOverride: func(_ ethdb.Database) triedb.DBOverride { return db },
	}
	return state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), triedbConfig)
}

// commitTrieDB commits a root through the standard TrieDB Update+Commit pipeline.
func commitTrieDB(t *testing.T, tdb *triedb.Database, root, parent common.Hash) {
	t.Helper()
	require.NoError(t, tdb.Update(root, parent, 0, nil, nil,
		stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1})))
	require.NoError(t, tdb.Commit(root, true))
}

// commitAccountState is a helper that writes an account through the standard
// accountTrie -> TrieDB pipeline and returns the committed root.
func commitAccountState(t *testing.T, db *TrieDB, parent common.Hash, addr common.Address, acct *types.StateAccount) common.Hash {
	t.Helper()
	internalDB := newInternalStateDB(t, db)
	stateDB := NewStateAccessor(internalDB, db)
	tr, err := stateDB.OpenTrie(parent)
	require.NoError(t, err)

	require.NoError(t, tr.UpdateAccount(addr, acct))
	root := tr.Hash()

	_, _, err = tr.Commit(true)
	require.NoError(t, err)
	commitTrieDB(t, stateDB.TrieDB(), root, parent)
	return root
}

// openReconstructedTrie creates a reconstructedAccountTrie from a committed root.
func openReconstructedTrie(t *testing.T, db *TrieDB, root common.Hash) *reconstructedAccountTrie {
	t.Helper()
	rev, err := db.Firewood.Revision(ffi.Hash(root))
	require.NoError(t, err)
	recon, err := rev.Reconstruct(nil)
	require.NoError(t, err)
	reconTrie, err := newReconstructedAccountTrie(recon)
	require.NoError(t, err)
	t.Cleanup(func() { reconTrie.Drop() })
	return reconTrie
}

func TestReconstructedTrieGetAccount(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Commit an account through the standard pipeline.
	addr := common.HexToAddress("0xABCD")
	acct := types.StateAccount{Balance: uint256.NewInt(42)}
	root := commitAccountState(t, db, types.EmptyRootHash, addr, &acct)

	// Open a reconstructed trie at that root.
	reconTrie := openReconstructedTrie(t, db, root)

	// Should be able to read the account.
	got, err := reconTrie.GetAccount(addr)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint256.NewInt(42), got.Balance)
}

func TestReconstructedTrieGetAccountMissing(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Commit one account.
	addr := common.HexToAddress("0xABCD")
	acct := types.StateAccount{Balance: uint256.NewInt(42)}
	root := commitAccountState(t, db, types.EmptyRootHash, addr, &acct)

	reconTrie := openReconstructedTrie(t, db, root)

	// A different address should return nil.
	missing := common.HexToAddress("0x9999")
	got, err := reconTrie.GetAccount(missing)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestReconstructedTrieUpdateAndHash(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Commit initial state.
	addr1 := common.HexToAddress("0x1111")
	acct1 := types.StateAccount{Balance: uint256.NewInt(100)}
	root1 := commitAccountState(t, db, types.EmptyRootHash, addr1, &acct1)

	// Open a reconstructed trie at root1.
	reconTrie := openReconstructedTrie(t, db, root1)

	// Add a second account and hash.
	addr2 := common.HexToAddress("0x2222")
	acct2 := types.StateAccount{Balance: uint256.NewInt(200)}
	require.NoError(t, reconTrie.UpdateAccount(addr2, &acct2))

	root2 := reconTrie.Hash()
	require.NotEqual(t, root1, root2)
	require.NotEqual(t, common.Hash{}, root2)

	// Both accounts should be readable after Hash().
	got1, err := reconTrie.GetAccount(addr1)
	require.NoError(t, err)
	require.NotNil(t, got1)
	require.Equal(t, uint256.NewInt(100), got1.Balance)

	got2, err := reconTrie.GetAccount(addr2)
	require.NoError(t, err)
	require.NotNil(t, got2)
	require.Equal(t, uint256.NewInt(200), got2.Balance)
}

func TestReconstructedTrieHashIdempotent(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	addr := common.HexToAddress("0x1111")
	acct := types.StateAccount{Balance: uint256.NewInt(10)}
	root := commitAccountState(t, db, types.EmptyRootHash, addr, &acct)

	reconTrie := openReconstructedTrie(t, db, root)

	// Add an account and hash twice -- second call should return cached root.
	addr2 := common.HexToAddress("0x2222")
	acct2 := types.StateAccount{Balance: uint256.NewInt(20)}
	require.NoError(t, reconTrie.UpdateAccount(addr2, &acct2))

	hash1 := reconTrie.Hash()
	hash2 := reconTrie.Hash() // no changes since last Hash()
	require.Equal(t, hash1, hash2)
}

func TestReconstructedTrieChainReconstruct(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Commit genesis state via standard trie.
	genesisAddr := common.HexToAddress("0xAAAA")
	genesisAcct := types.StateAccount{Balance: uint256.NewInt(1000)}
	root0 := commitAccountState(t, db, types.EmptyRootHash, genesisAddr, &genesisAcct)

	// Build a chain of 3 reconstructions from root0.
	reconTrie := openReconstructedTrie(t, db, root0)

	addrs := []common.Address{
		common.HexToAddress("0xBBBB"),
		common.HexToAddress("0xCCCC"),
		common.HexToAddress("0xDDDD"),
	}
	var roots []common.Hash
	for i, addr := range addrs {
		acct := types.StateAccount{Balance: uint256.NewInt(uint64(i + 1))}
		require.NoError(t, reconTrie.UpdateAccount(addr, &acct))
		root := reconTrie.Hash()
		require.NotEqual(t, common.Hash{}, root)
		roots = append(roots, root)
	}

	// All accounts (genesis + 3 new) should be readable.
	got, err := reconTrie.GetAccount(genesisAddr)
	require.NoError(t, err)
	require.Equal(t, uint256.NewInt(1000), got.Balance)

	for i, addr := range addrs {
		got, err := reconTrie.GetAccount(addr)
		require.NoError(t, err)
		require.Equal(t, uint256.NewInt(uint64(i+1)), got.Balance)
	}

	// Each successive root should be different.
	for i := 1; i < len(roots); i++ {
		require.NotEqual(t, roots[i-1], roots[i])
	}
}

func TestReconstructedTrieDeleteAccount(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	addr := common.HexToAddress("0xDEAD")
	acct := types.StateAccount{Balance: uint256.NewInt(500)}
	root := commitAccountState(t, db, types.EmptyRootHash, addr, &acct)

	reconTrie := openReconstructedTrie(t, db, root)

	// Account should exist.
	got, err := reconTrie.GetAccount(addr)
	require.NoError(t, err)
	require.NotNil(t, got)

	// Delete it.
	require.NoError(t, reconTrie.DeleteAccount(addr))

	// Should return nil from dirty cache.
	got, err = reconTrie.GetAccount(addr)
	require.NoError(t, err)
	require.Nil(t, got)

	// Storage for deleted account should also return nil.
	storageVal, err := reconTrie.GetStorage(addr, []byte{0x01})
	require.NoError(t, err)
	require.Nil(t, storageVal)
}

func TestReconstructedTrieStorage(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Commit an account with storage using the standard pipeline.
	internalDB := newInternalStateDB(t, db)
	stateDB := NewStateAccessor(internalDB, db)
	tr, err := stateDB.OpenTrie(types.EmptyRootHash)
	require.NoError(t, err)

	addr := common.HexToAddress("0x1234")
	acct := types.StateAccount{Balance: uint256.NewInt(100)}
	require.NoError(t, tr.UpdateAccount(addr, &acct))

	storageKey := []byte{0x01}
	storageVal := []byte{0x42}
	require.NoError(t, tr.UpdateStorage(addr, storageKey, storageVal))

	root := tr.Hash()
	_, _, err = tr.Commit(true)
	require.NoError(t, err)
	commitTrieDB(t, stateDB.TrieDB(), root, types.EmptyRootHash)

	// Open reconstructed trie and read storage.
	reconTrie := openReconstructedTrie(t, db, root)

	got, err := reconTrie.GetStorage(addr, storageKey)
	require.NoError(t, err)
	require.Equal(t, storageVal, got)

	// Update storage in the reconstructed trie.
	newVal := []byte{0x99}
	require.NoError(t, reconTrie.UpdateStorage(addr, storageKey, newVal))

	got, err = reconTrie.GetStorage(addr, storageKey)
	require.NoError(t, err)
	require.Equal(t, newVal, got)

	// Delete storage.
	require.NoError(t, reconTrie.DeleteStorage(addr, storageKey))

	got, err = reconTrie.GetStorage(addr, storageKey)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestReconstructedTrieCommit(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	addr := common.HexToAddress("0xAAAA")
	acct := types.StateAccount{Balance: uint256.NewInt(1)}
	root := commitAccountState(t, db, types.EmptyRootHash, addr, &acct)

	reconTrie := openReconstructedTrie(t, db, root)

	// Add an account and call Commit (which should just call Hash internally).
	addr2 := common.HexToAddress("0xBBBB")
	acct2 := types.StateAccount{Balance: uint256.NewInt(2)}
	require.NoError(t, reconTrie.UpdateAccount(addr2, &acct2))

	commitRoot, nodeSet, err := reconTrie.Commit(true)
	require.NoError(t, err)
	require.NotNil(t, nodeSet)
	require.NotEqual(t, common.Hash{}, commitRoot)
	require.NotEqual(t, root, commitRoot)
}

func TestReconstructedTrieCopy(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	addr := common.HexToAddress("0x1111")
	acct := types.StateAccount{Balance: uint256.NewInt(10)}
	root := commitAccountState(t, db, types.EmptyRootHash, addr, &acct)

	reconTrie := openReconstructedTrie(t, db, root)

	// Add a pending update.
	addr2 := common.HexToAddress("0x2222")
	acct2 := types.StateAccount{Balance: uint256.NewInt(20)}
	require.NoError(t, reconTrie.UpdateAccount(addr2, &acct2))

	// Copy the trie.
	copied := reconTrie.Copy()
	require.NotNil(t, copied)

	// The copy should see the same pending data.
	got, err := copied.GetAccount(addr2)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint256.NewInt(20), got.Balance)

	// Modifications to the copy should not affect the original.
	addr3 := common.HexToAddress("0x3333")
	acct3 := types.StateAccount{Balance: uint256.NewInt(30)}
	require.NoError(t, copied.UpdateAccount(addr3, &acct3))

	got, err = reconTrie.GetAccount(addr3)
	require.NoError(t, err)
	require.Nil(t, got) // original should not see addr3
}

func TestReconstructedTrieNilReconstructed(t *testing.T) {
	_, err := newReconstructedAccountTrie(nil)
	require.Error(t, err)
}

func TestReconstructedStorageTrieNoOps(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	addr := common.HexToAddress("0x1111")
	acct := types.StateAccount{Balance: uint256.NewInt(10)}
	root := commitAccountState(t, db, types.EmptyRootHash, addr, &acct)

	reconTrie := openReconstructedTrie(t, db, root)
	storageTrie := newReconstructedStorageTrie(reconTrie)

	// Storage trie Hash() returns empty.
	require.Equal(t, common.Hash{}, storageTrie.Hash())

	// Storage trie Commit() returns empty.
	commitRoot, nodeSet, err := storageTrie.Commit(true)
	require.NoError(t, err)
	require.Nil(t, nodeSet)
	require.Equal(t, common.Hash{}, commitRoot)

	// Storage trie Copy() returns nil.
	require.Nil(t, storageTrie.Copy())
}

func TestReconstructedTrieUnsupportedOps(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	addr := common.HexToAddress("0x1111")
	acct := types.StateAccount{Balance: uint256.NewInt(10)}
	root := commitAccountState(t, db, types.EmptyRootHash, addr, &acct)

	reconTrie := openReconstructedTrie(t, db, root)

	// NodeIterator should return error.
	_, err = reconTrie.NodeIterator(nil)
	require.Error(t, err)

	// Prove should return error.
	err = reconTrie.Prove(nil, nil)
	require.Error(t, err)

	// GetKey returns nil.
	require.Nil(t, reconTrie.GetKey([]byte("anything")))

	// UpdateContractCode is a no-op.
	require.NoError(t, reconTrie.UpdateContractCode(common.Address{}, common.Hash{}, nil))
}
