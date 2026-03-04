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

// chainedCommitter handles committing a sequence of blocks through the standard
// proposal pipeline, properly chaining parent/block hashes so that each
// subsequent Update finds the correct proposal in the possible map.
type chainedCommitter struct {
	t             *testing.T
	fw            *TrieDB
	lastBlockHash common.Hash
	height        uint64
}

func newChainedCommitter(t *testing.T, fw *TrieDB) *chainedCommitter {
	return &chainedCommitter{
		t:             t,
		fw:            fw,
		lastBlockHash: common.Hash{}, // genesis has empty block hash
		height:        0,
	}
}

// commitBlock opens a trie at the parent root, applies the given mutate
// function, hashes, commits, and returns the new root.
func (c *chainedCommitter) commitBlock(parentRoot common.Hash, mutate func(state.Trie)) common.Hash {
	c.t.Helper()
	triedbConfig := &triedb.Config{
		DBOverride: func(_ ethdb.Database) triedb.DBOverride { return c.fw },
	}
	internalDB := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), triedbConfig)
	stateDB := NewStateAccessor(internalDB, c.fw)

	tr, err := stateDB.OpenTrie(parentRoot)
	require.NoError(c.t, err)

	mutate(tr)

	root := tr.Hash()
	_, _, err = tr.Commit(true)
	require.NoError(c.t, err)

	// Use the previous block hash as parent and a unique block hash for this block.
	blockHash := common.BigToHash(new(uint256.Int).SetUint64(c.height + 1).ToBig())
	tdb := stateDB.TrieDB()
	require.NoError(c.t, tdb.Update(root, parentRoot, c.height, nil, nil,
		stateconf.WithTrieDBUpdatePayload(c.lastBlockHash, blockHash)))
	require.NoError(c.t, tdb.Commit(root, true))

	c.lastBlockHash = blockHash
	c.height++
	return root
}

// TestReconstructionChainMatchesCommits verifies that chaining Reconstruct()
// calls produces the same root hashes as committing through the standard
// proposal pipeline.
func TestReconstructionChainMatchesCommits(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	committer := newChainedCommitter(t, db)

	// Commit a sequence of blocks through the standard pipeline,
	// each adding one new account.
	addrs := make([]common.Address, 10)
	roots := make([]common.Hash, 10)
	prevRoot := types.EmptyRootHash

	for i := range 10 {
		addrs[i] = common.BigToAddress(new(uint256.Int).SetUint64(uint64(i + 1)).ToBig())
		acct := types.StateAccount{Balance: uint256.NewInt(uint64((i + 1) * 100))}
		addr := addrs[i]
		roots[i] = committer.commitBlock(prevRoot, func(tr state.Trie) {
			require.NoError(t, tr.UpdateAccount(addr, &acct))
		})
		prevRoot = roots[i]
	}

	// Now reconstruct the same chain from genesis.
	rev, err := db.Firewood.Revision(ffi.Hash(types.EmptyRootHash))
	require.NoError(t, err)

	recon, err := rev.Reconstruct(nil)
	require.NoError(t, err)
	reconTrie, err := newReconstructedAccountTrie(recon)
	require.NoError(t, err)

	for i := range 10 {
		acct := types.StateAccount{Balance: uint256.NewInt(uint64((i + 1) * 100))}
		require.NoError(t, reconTrie.UpdateAccount(addrs[i], &acct))
		reconRoot := reconTrie.Hash()

		// The reconstructed root should match the committed root.
		require.Equalf(t, roots[i], reconRoot,
			"root mismatch at block %d: committed=%s reconstructed=%s",
			i, roots[i].Hex(), reconRoot.Hex())

		// All accounts up to this point should be readable.
		for j := 0; j <= i; j++ {
			got, err := reconTrie.GetAccount(addrs[j])
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, uint256.NewInt(uint64((j+1)*100)), got.Balance)
		}
	}

	reconTrie.Drop()
}

// TestReconstructionWithStorageMatchesCommits verifies storage operations
// produce the same roots when reconstructed vs committed.
func TestReconstructionWithStorageMatchesCommits(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	committer := newChainedCommitter(t, db)

	addr := common.HexToAddress("0xAAAA")
	storageKey := []byte{0x01}

	// Block 1: create account with storage.
	acct := types.StateAccount{Balance: uint256.NewInt(1000), Nonce: 1}
	root1 := committer.commitBlock(types.EmptyRootHash, func(tr state.Trie) {
		require.NoError(t, tr.UpdateAccount(addr, &acct))
		require.NoError(t, tr.UpdateStorage(addr, storageKey, []byte{42}))
	})

	// Block 2: update storage value.
	root2 := committer.commitBlock(root1, func(tr state.Trie) {
		require.NoError(t, tr.UpdateStorage(addr, storageKey, []byte{99}))
	})

	// Reconstruct both blocks from genesis.
	rev, err := db.Firewood.Revision(ffi.Hash(types.EmptyRootHash))
	require.NoError(t, err)
	recon, err := rev.Reconstruct(nil)
	require.NoError(t, err)
	reconTrie, err := newReconstructedAccountTrie(recon)
	require.NoError(t, err)

	// Block 1 reconstruction.
	require.NoError(t, reconTrie.UpdateAccount(addr, &acct))
	require.NoError(t, reconTrie.UpdateStorage(addr, storageKey, []byte{42}))
	reconRoot1 := reconTrie.Hash()
	require.Equal(t, root1, reconRoot1)

	// Block 2 reconstruction.
	require.NoError(t, reconTrie.UpdateStorage(addr, storageKey, []byte{99}))
	reconRoot2 := reconTrie.Hash()
	require.Equal(t, root2, reconRoot2)

	// Verify storage reads.
	val, err := reconTrie.GetStorage(addr, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte{99}, val)

	reconTrie.Drop()
}

// TestReconstructionDeleteAndRecreate verifies that deleting an account and
// recreating it in subsequent blocks produces matching roots.
func TestReconstructionDeleteAndRecreate(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	committer := newChainedCommitter(t, db)

	addr := common.HexToAddress("0xDEAD")

	// Block 1: Create account.
	acct1 := types.StateAccount{Balance: uint256.NewInt(500), Nonce: 1}
	root1 := committer.commitBlock(types.EmptyRootHash, func(tr state.Trie) {
		require.NoError(t, tr.UpdateAccount(addr, &acct1))
	})

	// Block 2: Delete account.
	root2 := committer.commitBlock(root1, func(tr state.Trie) {
		require.NoError(t, tr.DeleteAccount(addr))
	})

	// Block 3: Recreate account with different balance.
	acct3 := types.StateAccount{Balance: uint256.NewInt(999), Nonce: 0}
	root3 := committer.commitBlock(root2, func(tr state.Trie) {
		require.NoError(t, tr.UpdateAccount(addr, &acct3))
	})

	// Reconstruct from genesis.
	rev, err := db.Firewood.Revision(ffi.Hash(types.EmptyRootHash))
	require.NoError(t, err)
	recon, err := rev.Reconstruct(nil)
	require.NoError(t, err)
	reconTrie, err := newReconstructedAccountTrie(recon)
	require.NoError(t, err)

	// Block 1.
	require.NoError(t, reconTrie.UpdateAccount(addr, &acct1))
	require.Equal(t, root1, reconTrie.Hash())

	// Block 2.
	require.NoError(t, reconTrie.DeleteAccount(addr))
	require.Equal(t, root2, reconTrie.Hash())

	// Block 3.
	require.NoError(t, reconTrie.UpdateAccount(addr, &acct3))
	require.Equal(t, root3, reconTrie.Hash())

	// Final state should show the recreated account.
	got, err := reconTrie.GetAccount(addr)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint256.NewInt(999), got.Balance)

	reconTrie.Drop()
}
