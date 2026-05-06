// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestReconstructedRevisions(t *testing.T) {
	r := require.New(t)

	dbDir := t.TempDir()
	cfg := DefaultConfig(dbDir)

	// Set commitInterval high to persist only on shutdown
	commitInterval := uint64(500)
	cfg.DeferredCommitInterval = commitInterval
	cfg.RevisionsInMemory = uint(commitInterval)

	db := newTestDatabaseWithConfig(t, cfg)
	trie, err := db.OpenTrie(types.EmptyRootHash)
	r.NoError(err)

	var (
		addr           = common.HexToAddress("1234")
		initialBalance = uint256.NewInt(100)
	)

	r.NoError(trie.UpdateAccount(addr, &types.StateAccount{Balance: initialBalance}))

	// Commit and persist initial revision (R1)
	initialRoot, _, err := trie.Commit(true)
	r.NoError(err)
	r.NoError(db.TrieDB().Update(
		initialRoot,
		types.EmptyRootHash,
		0,
		nil,
		nil,
		stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1})),
	)
	r.NoError(db.TrieDB().Commit(initialRoot, true))
	r.NoError(db.TrieDB().Backend().Close())

	// Reopen the database
	db = newTestDatabaseWithConfig(t, cfg)

	// Create a reconstructed view starting from R1 (the latest persisted revision)
	tdb := db.TrieDB().Backend().(*TrieDB)
	rev, err := tdb.Firewood.LatestRevision()
	r.NoError(err)
	recon, err := rev.Reconstruct(nil)
	r.NoError(err)
	r.NoError(rev.Drop())
	t.Cleanup(func() {
		r.NoError(recon.Drop())
	})

	reconDB, err := NewReconstructedStateAccessor(db, recon, true /* computeRootOnHash */)
	r.NoError(err)

	var (
		newBalances = []*uint256.Int{
			uint256.NewInt(200),
			uint256.NewInt(300),
			uint256.NewInt(400),
		}
		prevRoot      = initialRoot
		prevBlockHash = common.Hash{} // After reopen, TrieDB only knows the empty block hash.
	)

	// Commit 3 more times (R2, R3, R4)
	// Afterwards, reconstruct R2, R3, R4 from R1 and verify the roots match.
	for i, balance := range newBalances {
		// Commit normal revision.
		trie, err = db.OpenTrie(prevRoot)
		r.NoError(err)

		r.NoError(trie.UpdateAccount(addr, &types.StateAccount{Balance: balance}))
		normalRoot := trie.Hash()
		_, _, err = trie.Commit(true)
		r.NoError(err)

		blockHash := common.Hash{byte(i + 1)}
		r.NoError(db.TrieDB().Update(
			normalRoot,
			prevRoot,
			uint64(i),
			nil,
			nil,
			stateconf.WithTrieDBUpdatePayload(prevBlockHash, blockHash)),
		)
		r.NoError(db.TrieDB().Commit(normalRoot, true))
		prevRoot = normalRoot
		prevBlockHash = blockHash

		// Create reconstructed revision and verify root matches.
		reconTrie, err := reconDB.OpenTrie(common.Hash(recon.Root()))
		r.NoError(err)
		r.NoError(reconTrie.UpdateAccount(addr, &types.StateAccount{Balance: balance}))
		r.Equal(normalRoot, reconTrie.Hash(), "reconstructed root mismatch for R%d", i+2)
	}
}

// TestReconstructedRevisionHashing verifies computeRootOnHash=false only
// flushes writes without computing the reconstructed root.
func TestReconstructedRevisionHashing(t *testing.T) {
	db := newTestDatabase(t)
	trie, err := db.OpenTrie(types.EmptyRootHash)
	require.NoError(t, err)

	// Start by committing an initial revision to build reconstructed revisions from.
	addr := common.HexToAddress("1234")
	require.NoError(t, trie.UpdateAccount(addr, &types.StateAccount{Balance: uint256.NewInt(100)}))

	initialRoot, _, err := trie.Commit(true)
	require.NoError(t, err)

	require.NoError(t, db.TrieDB().Update(
		initialRoot,
		types.EmptyRootHash,
		0,
		nil,
		nil,
		stateconf.WithTrieDBUpdatePayload(common.Hash{}, common.Hash{1})),
	)
	require.NoError(t, db.TrieDB().Commit(initialRoot, true))

	tdb := db.TrieDB().Backend().(*TrieDB)
	rev, err := tdb.Firewood.LatestRevision()
	require.NoError(t, err)
	recon, err := rev.Reconstruct(nil)
	require.NoError(t, err)
	require.NoError(t, rev.Drop())
	t.Cleanup(func() {
		require.NoError(t, recon.Drop())
	})

	// First, use the reconstructed view with computeRootOnHash=false so that Hash
	// flushes pending writes but returns the cached root.
	replayAccessor, err := NewReconstructedStateAccessor(db, recon, false /* computeRootOnHash */)
	require.NoError(t, err)
	replayTrie, err := replayAccessor.OpenTrie(initialRoot)
	require.NoError(t, err)
	require.NoError(t, replayTrie.UpdateAccount(addr, &types.StateAccount{Balance: uint256.NewInt(200)}))

	require.Equal(t, initialRoot, replayTrie.Hash())
	require.NotEqual(t, initialRoot, common.Hash(recon.Root()))
}
