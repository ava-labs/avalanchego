// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/firewood/statehistory"
)

// commitBlock drives one block through the propose/commit path: parent state
// root + mutations -> Update -> Commit, returning the new state root.
func commitBlock(
	t *testing.T,
	db state.Database,
	parentRoot common.Hash,
	height uint64,
	parentBlockHash, blockHash common.Hash,
	mutate func(t *testing.T, tr state.Trie),
) common.Hash {
	t.Helper()
	tr, err := db.OpenTrie(parentRoot)
	require.NoError(t, err)
	if mutate != nil {
		mutate(t, tr)
	}
	root, _, err := tr.Commit(true)
	require.NoError(t, err)

	triedb := db.TrieDB()
	require.NoError(t, triedb.Update(
		root, parentRoot, height, nil, nil,
		stateconf.WithTrieDBUpdatePayload(parentBlockHash, blockHash),
	))
	require.NoError(t, triedb.Commit(root, true))
	return root
}

func requireWatermarks(t *testing.T, store *statehistory.Store, wantFirst, wantHead uint64) {
	t.Helper()
	first, ok, err := store.FirstBlock()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, wantFirst, first)

	head, ok, err := store.Head()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, wantHead, head)
}

// historicalState opens a StateDB over the flat history at the given target
// block, exactly as the eth serve path does.
func historicalState(t *testing.T, store *statehistory.Store, frontier state.Database, target uint64, root common.Hash) *state.StateDB {
	t.Helper()
	overlay := statehistory.NewOverlay(store, frontier, target, root)
	sdb, err := state.New(root, overlay, nil)
	require.NoError(t, err)
	return sdb
}

// TestStateHistoryCapture pins that every committed block's changes flow into
// the history store via the propose/commit path, including the genesis block
// (block 0), empty blocks, and account destruction, and that uncommitted
// sibling proposals are never flushed.
func TestStateHistoryCapture(t *testing.T) {
	require := require.New(t)

	cfg := DefaultConfig(t.TempDir())
	cfg.EnableStateHistory = true
	db := newTestDatabaseWithConfig(t, cfg)

	store := db.TrieDB().Backend().(*TrieDB).HistoryStore()
	require.NotNil(store)

	var (
		addr1 = common.HexToAddress("0x1111")
		addr2 = common.HexToAddress("0x2222")
		slot  = common.Hash{0x01}
		acct1 = generateAccount(addr1)
		acct2 = generateAccount(addr2)

		hash0 = common.Hash{0xa0}
		hash1 = common.Hash{0xa1}
		hash2 = common.Hash{0xa2}
		hash3 = common.Hash{0xa3}
	)

	// Genesis (block 0) flows through the same propose/commit path and seeds
	// the history baseline.
	root0 := commitBlock(t, db, types.EmptyRootHash, 0, common.Hash{}, hash0, func(t *testing.T, tr state.Trie) {
		require.NoError(tr.UpdateAccount(addr1, &acct1))
		require.NoError(tr.UpdateStorage(addr1, slot[:], []byte{0x77}))
	})
	requireWatermarks(t, store, 0, 0)

	// Block 1: modify the slot, create a second account with storage.
	root1 := commitBlock(t, db, root0, 1, hash0, hash1, func(t *testing.T, tr state.Trie) {
		require.NoError(tr.UpdateStorage(addr1, slot[:], []byte{0x99}))
		require.NoError(tr.UpdateAccount(addr2, &acct2))
		require.NoError(tr.UpdateStorage(addr2, slot[:], []byte{0x22}))
	})
	requireWatermarks(t, store, 0, 1)

	// Block 2: destroy the second account.
	root2 := commitBlock(t, db, root1, 2, hash1, hash2, func(t *testing.T, tr state.Trie) {
		require.NoError(tr.DeleteAccount(addr2))
	})
	requireWatermarks(t, store, 0, 2)

	// Block 3: empty block (same root); history head must still advance to
	// keep the range contiguous.
	root3 := commitBlock(t, db, root2, 3, hash2, hash3, nil)
	require.Equal(root2, root3)
	requireWatermarks(t, store, 0, 3)

	// Read the history back through the overlay, as of each height.
	sdb0 := historicalState(t, store, db, 0, root0)
	require.Equal(acct1.Balance, sdb0.GetBalance(addr1))
	require.Equal(common.Hash{31: 0x77}, sdb0.GetState(addr1, slot))
	require.False(sdb0.Exist(addr2))

	sdb1 := historicalState(t, store, db, 1, root1)
	require.Equal(common.Hash{31: 0x99}, sdb1.GetState(addr1, slot))
	require.Equal(acct2.Balance, sdb1.GetBalance(addr2))
	require.Equal(common.Hash{31: 0x22}, sdb1.GetState(addr2, slot))

	sdb2 := historicalState(t, store, db, 2, root2)
	require.Equal(common.Hash{31: 0x99}, sdb2.GetState(addr1, slot))
	require.False(sdb2.Exist(addr2))
	require.Equal(common.Hash{}, sdb2.GetState(addr2, slot))
}

// TestStateHistorySkipsUncommittedProposals pins that a competing proposal at
// the same height leaves no rows behind: only the committed branch is flushed.
func TestStateHistorySkipsUncommittedProposals(t *testing.T) {
	require := require.New(t)

	cfg := DefaultConfig(t.TempDir())
	cfg.EnableStateHistory = true
	db := newTestDatabaseWithConfig(t, cfg)
	triedb := db.TrieDB()

	store := triedb.Backend().(*TrieDB).HistoryStore()
	require.NotNil(store)

	addr := common.HexToAddress("0x1111")
	acct := generateAccount(addr)
	winnerSlot := common.Hash{0x0a}
	loserSlot := common.Hash{0x0b}

	root0 := commitBlock(t, db, types.EmptyRootHash, 0, common.Hash{}, common.Hash{0xa0}, func(t *testing.T, tr state.Trie) {
		require.NoError(tr.UpdateAccount(addr, &acct))
	})

	// Two competing blocks at height 1.
	makeChild := func(slot common.Hash, blockHash common.Hash) common.Hash {
		tr, err := db.OpenTrie(root0)
		require.NoError(err)
		require.NoError(tr.UpdateStorage(addr, slot[:], []byte{0x01}))
		root, _, err := tr.Commit(true)
		require.NoError(err)
		require.NoError(triedb.Update(
			root, root0, 1, nil, nil,
			stateconf.WithTrieDBUpdatePayload(common.Hash{0xa0}, blockHash),
		))
		return root
	}
	winnerRoot := makeChild(winnerSlot, common.Hash{0xb1})
	makeChild(loserSlot, common.Hash{0xb2})

	require.NoError(triedb.Commit(winnerRoot, true))
	requireWatermarks(t, store, 0, 1)

	sdb := historicalState(t, store, db, 1, winnerRoot)
	require.Equal(common.Hash{31: 0x01}, sdb.GetState(addr, winnerSlot))
	require.Equal(common.Hash{}, sdb.GetState(addr, loserSlot))
}
