// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func newTestDatabase(t *testing.T) state.Database {
	t.Helper()
	fwConfig := DefaultConfig(t.TempDir())
	triedbConfig := &triedb.Config{
		DBOverride: fwConfig.BackendConstructor,
	}
	internalState := state.NewDatabaseWithConfig(
		rawdb.NewMemoryDatabase(),
		triedbConfig,
	)
	tdb := internalState.TrieDB().Backend().(*TrieDB)
	t.Cleanup(func() {
		require.NoError(t, tdb.Close())
	})

	return NewStateAccessor(internalState, tdb)
}

func TestCommitEmptyGenesis(t *testing.T) {
	db := newTestDatabase(t)
	triedb := db.TrieDB()

	tr, err := db.OpenTrie(types.EmptyRootHash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)

	root := tr.Hash()
	require.Equal(t, types.EmptyRootHash, root)

	root, _, err = tr.Commit(true)
	require.NoErrorf(t, err, "%T.Commit()", tr)
	require.Equal(t, types.EmptyRootHash, root)

	require.NoErrorf(
		t,
		triedb.Update(
			types.EmptyRootHash,
			types.EmptyRootHash,
			0, nil, nil,
			stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1}),
		),
		"%T.Update()", triedb,
	)

	require.NoErrorf(t, triedb.Commit(types.EmptyRootHash, true), "%T.Commit()", triedb)
}

func generateAccount(addr common.Address) types.StateAccount {
	return types.StateAccount{
		Balance: uint256.NewInt(0).SetBytes(addr[:]),
	}
}

func verifyAccount(t *testing.T, tr state.Trie, addr common.Address, expected types.StateAccount) {
	t.Helper()
	acct, err := tr.GetAccount(addr)
	require.NoErrorf(t, err, "%T.GetAccount(%s)", tr, addr)
	require.Equalf(t, expected.Balance, acct.Balance, "%T.GetAccount(%s) balance", tr, addr)
	require.Equalf(t, expected.Nonce, acct.Nonce, "%T.GetAccount(%s) nonce", tr, addr)
}

func TestAccountPersistence(t *testing.T) {
	db := newTestDatabase(t)
	triedb := db.TrieDB()

	tr, err := db.OpenTrie(types.EmptyRootHash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)
	require.NotNil(t, tr)

	addr := common.HexToAddress("1234")
	acct := generateAccount(addr)
	require.NoErrorf(t, tr.UpdateAccount(addr, &acct), "%T.UpdateAccount()", tr)
	verifyAccount(t, tr, addr, acct)

	hash := tr.Hash()
	require.NotEqual(t, types.EmptyRootHash, hash)

	root, _, err := tr.Commit(true)
	require.NoErrorf(t, err, "%T.Commit()", tr)
	require.Equal(t, hash, root)

	require.NoErrorf(
		t,
		triedb.Update(
			hash,
			types.EmptyRootHash,
			0, nil, nil,
			stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1}),
		),
		"%T.Update()", triedb,
	)

	// We should be able to read the account before and after committing.
	tr, err = db.OpenTrie(hash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)
	verifyAccount(t, tr, addr, acct)

	require.NoErrorf(t, triedb.Commit(hash, true), "%T.Commit()", triedb)
	tr, err = db.OpenTrie(hash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)
	verifyAccount(t, tr, addr, acct)
}

func TestStoragePersistence(t *testing.T) {
	db := newTestDatabase(t)
	triedb := db.TrieDB()

	tr, err := db.OpenTrie(types.EmptyRootHash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)
	require.NotNil(t, tr)

	addr := common.HexToAddress("1234")
	acct := generateAccount(addr)
	require.NoErrorf(t, tr.UpdateAccount(addr, &acct), "%T.UpdateAccount()", tr)

	key := []byte{1}
	value := []byte{2}
	// A separate Storage Trie is expected to be used.
	require.NoErrorf(t, tr.UpdateStorage(addr, key, value), "%T.UpdateStorage()", tr)

	// Retrievable before hashing
	storedValue, err := tr.GetStorage(addr, key)
	require.NoErrorf(t, err, "%T.GetStorage()", tr)
	require.Equalf(t, value, storedValue, "%T.GetStorage() value", tr)

	hash := tr.Hash()
	require.NotEqual(t, types.EmptyRootHash, hash)

	root, _, err := tr.Commit(true)
	require.NoErrorf(t, err, "%T.Commit()", tr)
	require.Equal(t, hash, root)

	require.NoErrorf(
		t,
		triedb.Update(
			hash,
			types.EmptyRootHash,
			0, nil, nil,
			stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1}),
		),
		"%T.Update()", triedb,
	)

	// We should be able to read the storage before and after committing.
	tr, err = db.OpenTrie(hash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)
	verifyAccount(t, tr, addr, acct)

	storedValue, err = tr.GetStorage(addr, key)
	require.NoErrorf(t, err, "%T.GetStorage()", tr)
	require.Equalf(t, value, storedValue, "%T.GetStorage() value", tr)

	require.NoErrorf(t, triedb.Commit(hash, true), "%T.Commit()", triedb)
	tr, err = db.OpenTrie(hash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)
	verifyAccount(t, tr, addr, acct)

	storedValue, err = tr.GetStorage(addr, key)
	require.NoErrorf(t, err, "%T.GetStorage()", tr)
	require.Equalf(t, value, storedValue, "%T.GetStorage() value", tr)
}

// Ensure that even if other tries are hashed, others can still be persisted via Update.
// Additionally, the now stale tries should not be accessible.
func TestParallelHashing(t *testing.T) {
	db := newTestDatabase(t)
	triedb := db.TrieDB()

	tr1, err := db.OpenTrie(types.EmptyRootHash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)
	require.NotNil(t, tr1)

	addr1 := common.HexToAddress("1234")
	acct1 := generateAccount(addr1)
	require.NoErrorf(t, tr1.UpdateAccount(addr1, &acct1), "%T.UpdateAccount()", tr1)

	tr2, err := db.OpenTrie(types.EmptyRootHash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)
	require.NotNil(t, tr2)

	addr2 := common.HexToAddress("5678")
	acct2 := generateAccount(addr2)
	require.NoErrorf(t, tr2.UpdateAccount(addr2, &acct2), "%T.UpdateAccount()", tr2)

	hash1 := tr1.Hash()
	hash2 := tr2.Hash()
	require.NotEqual(t, hash1, hash2)

	// Commit both tries
	root1, _, err := tr1.Commit(true)
	require.NoErrorf(t, err, "%T.Commit()", tr1)
	require.Equal(t, hash1, root1)
	root2, _, err := tr2.Commit(true)
	require.NoErrorf(t, err, "%T.Commit()", tr2)
	require.Equal(t, hash2, root2)

	require.NoErrorf(
		t,
		triedb.Update(
			hash1,
			types.EmptyRootHash,
			0, nil, nil,
			stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1}),
		),
		"%T.Update()", triedb,
	)

	err = triedb.Update(
		hash2,
		types.EmptyRootHash,
		0, nil, nil,
		stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1}),
	)
	require.ErrorIsf(t, err, errNoProposalFound, "%T.Update()", triedb)
}

func TestUpdateWithWrongParameters(t *testing.T) {
	db := newTestDatabase(t)
	triedb := db.TrieDB()

	tr, err := db.OpenTrie(types.EmptyRootHash)
	require.NoErrorf(t, err, "%T.OpenTrie()", db)
	require.NotNil(t, tr)

	addr := common.HexToAddress("1234")
	acct := generateAccount(addr)
	require.NoErrorf(t, tr.UpdateAccount(addr, &acct), "%T.UpdateAccount()", tr)

	hash := tr.Hash()
	require.NotEqual(t, types.EmptyRootHash, hash)

	root, _, err := tr.Commit(true)
	require.NoErrorf(t, err, "%T.Commit()", tr)
	require.Equal(t, hash, root)

	// "Accidentally" provide the wrong height
	err = triedb.Update(
		root,
		types.EmptyRootHash,
		42, nil, nil,
		stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1}),
	)
	require.ErrorIsf(t, err, errUnexpectedProposalFound, "%T.Update()", triedb)

	// Providing the correct parameters can recover
	require.NoErrorf(
		t,
		triedb.Update(
			root,
			types.EmptyRootHash,
			0, nil, nil,
			stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1}),
		),
		"%T.Update()", triedb,
	)
	require.NoErrorf(t, triedb.Commit(root, true), "%T.Commit()", triedb)
}
