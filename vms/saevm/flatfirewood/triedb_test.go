// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flatfirewood

import (
	"path/filepath"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/firewood"
)

// newDB opens a flatfirewood-backed state database under dir that keeps only
// the last two Firewood revisions, so every older root can only be served
// from the history.
func newDB(t *testing.T, diskdb ethdb.Database, dir string) state.Database {
	fw := firewood.DefaultConfig(filepath.Join(dir, "firewood"), loggingtest.New(t, logging.Debug))
	fw.RevisionsInMemory = 2
	fw.DeferredCommitInterval = 1
	cfg := Config{Firewood: fw, Path: filepath.Join(dir, Directory)}
	db := state.NewDatabaseWithConfig(diskdb, &triedb.Config{DBOverride: cfg.BackendConstructor})
	require.IsType(t, (*TrieDB)(nil), db.TrieDB().Backend())
	return db
}

type chain struct {
	t     *testing.T
	db    state.Database
	root  common.Hash
	roots []common.Hash // post-execution root per block
}

func (c *chain) commit(mutate func(*state.StateDB)) {
	sdb, err := state.New(c.root, c.db, nil)
	require.NoError(c.t, err)
	mutate(sdb)
	block := uint64(len(c.roots))
	root, err := sdb.Commit(block, true)
	require.NoError(c.t, err, "StateDB.Commit(%d)", block)
	require.NoError(c.t, c.db.TrieDB().Commit(root, false), "TrieDB.Commit(%d)", block)
	c.root = root
	c.roots = append(c.roots, root)
}

func TestHistoricalReads(t *testing.T) {
	var (
		diskdb = rawdb.NewMemoryDatabase()
		dir    = t.TempDir()
		addr   = common.Address{1}
		other  = common.Address{2}
		slot   = common.Hash{3}
	)
	c := &chain{t: t, db: newDB(t, diskdb, dir), root: types.EmptyRootHash}
	c.commit(func(s *state.StateDB) { s.SetBalance(addr, uint256.NewInt(100)) })
	c.commit(func(s *state.StateDB) { s.SetState(addr, slot, common.Hash{7}) })
	c.commit(func(s *state.StateDB) { s.SetBalance(other, uint256.NewInt(1)) }) // addr untouched
	c.commit(func(s *state.StateDB) { s.SetBalance(addr, uint256.NewInt(200)) })
	c.commit(func(s *state.StateDB) {
		s.SelfDestruct(addr)
		s.SetBalance(other, uint256.NewInt(2))
	})
	c.commit(func(s *state.StateDB) { s.SetBalance(addr, uint256.NewInt(300)) }) // recreated, storage gone

	wantBalance := []uint64{100, 100, 100, 200, 0, 300}
	wantSlot := []common.Hash{{}, {7}, {7}, {7}, {}, {}}
	check := func(t *testing.T, db state.Database) {
		for h, root := range c.roots {
			sdb, err := state.New(root, db, nil)
			require.NoError(t, err, "state.New(root of block %d)", h)
			require.Equal(t, wantBalance[h], sdb.GetBalance(addr).Uint64(), "balance at block %d", h)
			require.Equal(t, wantSlot[h], sdb.GetState(addr, slot), "slot at block %d", h)
		}
		// Only the last two roots are still in Firewood.
		_, err := state.New(c.roots[0], db, nil)
		require.NoError(t, err)
		var missing *trie.MissingNodeError
		_, err = state.New(common.Hash{0xff}, db, nil)
		require.ErrorAs(t, err, &missing, "unknown root")
	}
	t.Run("live", func(t *testing.T) { check(t, c.db) })

	require.NoError(t, c.db.TrieDB().Close())
	t.Run("reopened", func(t *testing.T) {
		db := newDB(t, diskdb, dir)
		t.Cleanup(func() { require.NoError(t, db.TrieDB().Close()) })
		check(t, db)
	})
}

func TestRefusesFirewoodStateWithoutHistory(t *testing.T) {
	dir := t.TempDir()
	c := &chain{t: t, db: newDB(t, rawdb.NewMemoryDatabase(), dir), root: types.EmptyRootHash}
	c.commit(func(s *state.StateDB) { s.SetBalance(common.Address{1}, uint256.NewInt(1)) })
	require.NoError(t, c.db.TrieDB().Close())

	fw := firewood.DefaultConfig(filepath.Join(dir, "firewood"), loggingtest.New(t, logging.Debug))
	_, err := New(rawdb.NewMemoryDatabase(), Config{Firewood: fw, Path: filepath.Join(dir, "empty-history")})
	require.ErrorIs(t, err, ErrNoBaseline)
}

func TestFlushIdempotentAndFirstHeightWins(t *testing.T) {
	c := &chain{t: t, db: newDB(t, rawdb.NewMemoryDatabase(), t.TempDir()), root: types.EmptyRootHash}
	t.Cleanup(func() { require.NoError(t, c.db.TrieDB().Close()) })
	c.commit(func(s *state.StateDB) { s.SetBalance(common.Address{1}, uint256.NewInt(1)) })
	s := c.db.TrieDB().Backend().(*TrieDB).store

	// Re-executing block 0 after a crash rewrites identical rows.
	require.NoError(t, s.flush(c.roots[0], 0, []op{{kind: opPut, key: hashedAccountKey(common.Address{1}), value: []byte{0xc0}}}))
	// The same root produced again later keeps its first height.
	require.NoError(t, s.flush(c.roots[0], 7, nil))
	h, ok, err := s.heightOf(c.roots[0])
	require.NoError(t, err)
	require.True(t, ok)
	require.Zero(t, h)
}
