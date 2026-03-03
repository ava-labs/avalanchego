// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"testing"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestReconstructedStateAccessorOpenTrie(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Commit an account.
	internalDB := newInternalStateDB(t, db)
	stateDB := NewStateAccessor(internalDB, db)
	tr, err := stateDB.OpenTrie(types.EmptyRootHash)
	require.NoError(t, err)

	addr := common.HexToAddress("0x1234")
	acct := types.StateAccount{Balance: uint256.NewInt(500)}
	require.NoError(t, tr.UpdateAccount(addr, &acct))
	root := tr.Hash()
	_, _, err = tr.Commit(true)
	require.NoError(t, err)
	commitTrieDB(t, stateDB.TrieDB(), root, types.EmptyRootHash)

	// Create a Reconstructed at that root.
	rev, err := db.Firewood.Revision(ffi.Hash(root))
	require.NoError(t, err)
	recon, err := rev.Reconstruct(nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recon.Drop()) })

	// Build a reconstructed state accessor.
	accessor := NewReconstructedStateAccessor(internalDB, recon)

	// OpenTrie should return a reconstructedAccountTrie.
	reconTrie, err := accessor.OpenTrie(common.Hash(recon.Root()))
	require.NoError(t, err)

	got, err := reconTrie.GetAccount(addr)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint256.NewInt(500), got.Balance)
}

func TestReconstructedStateAccessorOpenStorageTrie(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Commit an account with storage.
	internalDB := newInternalStateDB(t, db)
	stateDB := NewStateAccessor(internalDB, db)
	tr, err := stateDB.OpenTrie(types.EmptyRootHash)
	require.NoError(t, err)

	addr := common.HexToAddress("0x5678")
	acct := types.StateAccount{Balance: uint256.NewInt(100)}
	require.NoError(t, tr.UpdateAccount(addr, &acct))
	storageKey := []byte{0x01}
	storageVal := []byte{42}
	require.NoError(t, tr.UpdateStorage(addr, storageKey, storageVal))
	root := tr.Hash()
	_, _, err = tr.Commit(true)
	require.NoError(t, err)
	commitTrieDB(t, stateDB.TrieDB(), root, types.EmptyRootHash)

	// Create Reconstructed.
	rev, err := db.Firewood.Revision(ffi.Hash(root))
	require.NoError(t, err)
	recon, err := rev.Reconstruct(nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recon.Drop()) })

	accessor := NewReconstructedStateAccessor(internalDB, recon)

	// Open account trie, then storage trie.
	acctTrie, err := accessor.OpenTrie(root)
	require.NoError(t, err)

	storageTrie, err := accessor.OpenStorageTrie(root, addr, common.Hash{}, acctTrie)
	require.NoError(t, err)
	require.NotNil(t, storageTrie)

	// Read storage through the storage trie.
	val, err := storageTrie.GetStorage(addr, storageKey)
	require.NoError(t, err)
	require.Equal(t, storageVal, val)
}

func TestReconstructedStateAccessorCopyTrie(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Simple setup.
	internalDB := newInternalStateDB(t, db)
	stateDB := NewStateAccessor(internalDB, db)
	tr, err := stateDB.OpenTrie(types.EmptyRootHash)
	require.NoError(t, err)
	addr := common.HexToAddress("0xAAAA")
	acct := types.StateAccount{Balance: uint256.NewInt(999)}
	require.NoError(t, tr.UpdateAccount(addr, &acct))
	root := tr.Hash()
	_, _, err = tr.Commit(true)
	require.NoError(t, err)
	commitTrieDB(t, stateDB.TrieDB(), root, types.EmptyRootHash)

	// Create Reconstructed.
	rev, err := db.Firewood.Revision(ffi.Hash(root))
	require.NoError(t, err)
	recon, err := rev.Reconstruct(nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recon.Drop()) })

	accessor := NewReconstructedStateAccessor(internalDB, recon)

	// Open and copy the trie.
	acctTrie, err := accessor.OpenTrie(root)
	require.NoError(t, err)

	copiedTrie := accessor.CopyTrie(acctTrie)
	require.NotNil(t, copiedTrie)

	// Copied trie should be able to read the account.
	got, err := copiedTrie.GetAccount(addr)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint256.NewInt(999), got.Balance)

	// CopyTrie of a storage trie should return nil.
	storageTrie, err := accessor.OpenStorageTrie(root, addr, common.Hash{}, acctTrie)
	require.NoError(t, err)
	require.Nil(t, accessor.CopyTrie(storageTrie))
}
