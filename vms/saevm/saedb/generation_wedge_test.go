// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"
)

// These tests reproduce the "generation wedge": snapshot generation verifies
// ranges against the trie at the snapshot disk layer's root, libevm's
// [state.StateDB.Commit] pins that root 128 blocks behind the execution head
// (snaps.Cap(root, 128)), and SAE's settlement-based pruning dereferences
// tries after ~params.Tau — a handful of blocks. The disk root's trie is
// therefore always already pruned: generation dies on its first range proof,
// is restarted by the next flatten against the next (equally pruned) root,
// and never completes. While its marker is set, [snapshot.Tree] refuses to
// construct iterators ([snapshot.ErrNotConstructed]), disabling the snapshot
// for state sync serving and execution reads alike.
//
// The wedged test recreates the steady state directly — an in-progress
// generator whose disk root has no resolvable trie — and drives the same
// Update/Cap flatten cycles execution drives. The healthy control differs in
// exactly one respect: the disk root's trie is on disk. Together they pin the
// pruned trie, not the flatten cycles, as the wedging factor.
//
// SAE no longer enters the wedged steady state: [Tracker.StateDBCommitOptions]
// caps the snapshot tree at [SnapshotCapLayers] (=1) so the disk layer's root
// is at most one block behind the head — a trie that is still referenced —
// and [Tracker.PinSnapshotDiskRoot] keeps it referenced until the disk layer
// advances. The wedged test below remains as documentation of the libevm
// behaviour that makes those two measures necessary.

// writeGenerator persists an in-progress generation marker, as a crashed or
// perpetually-failing generator leaves behind.
func writeGenerator(t *testing.T, db ethdb.Database, gen generatorState) {
	t.Helper()
	blob, err := rlp.EncodeToBytes(gen)
	require.NoError(t, err, "rlp.EncodeToBytes(generatorState)")
	rawdb.WriteSnapshotGenerator(db, blob)
}

func readGenerator(t *testing.T, db ethdb.Database) generatorState {
	t.Helper()
	blob := rawdb.ReadSnapshotGenerator(db)
	require.NotEmpty(t, blob, "rawdb.ReadSnapshotGenerator()")
	var gen generatorState
	require.NoError(t, rlp.DecodeBytes(blob, &gen), "rlp.DecodeBytes() of generator entry")
	return gen
}

// tryReadGenerator is a non-requiring counterpart to readGenerator, safe to
// call from a [require.Eventually] condition, which runs on a goroutine other
// than the test's own; a require failure there would call runtime.Goexit on
// the wrong goroutine instead of failing the test.
func tryReadGenerator(db ethdb.Database) (gen generatorState, ok bool) {
	blob := rawdb.ReadSnapshotGenerator(db)
	if len(blob) == 0 {
		return generatorState{}, false
	}
	if err := rlp.DecodeBytes(blob, &gen); err != nil {
		return generatorState{}, false
	}
	return gen, true
}

// flattenCycles emulates execution's per-block snapshot maintenance: push a
// diff layer, flatten it to disk (advancing the disk root, and restarting any
// in-progress generation against it — snapshot.go's diffToDisk).
func flattenCycles(t *testing.T, snaps *snapshot.Tree, parent common.Hash, n int) common.Hash {
	t.Helper()
	for i := range n {
		var child common.Hash
		binary.BigEndian.PutUint64(child[:8], uint64(i)+1) //#nosec G115 -- test loop index
		child[8] = 0xfe                                    // disambiguate from any real root

		require.NoError(t,
			snaps.Update(child, parent, nil, map[common.Hash][]byte{}, nil),
			"snapshot.Tree.Update() cycle %d", i,
		)
		require.NoError(t, snaps.Cap(child, 0), "snapshot.Tree.Cap() cycle %d", i)
		parent = child
	}
	return parent
}

func TestSnapshotGenerationWedgedByPrunedDiskRootTrie(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, nil)

	// The steady state on a pruning SAE node: the persisted disk root is a
	// recent post-execution root whose trie has been dereferenced — no node
	// of it is resolvable — and generation is in progress from key zero.
	prunedRoot := common.HexToHash("0xdeadbeef00000000000000000000000000000000000000000000000000000001")
	rawdb.WriteSnapshotRoot(db, prunedRoot)
	writeGenerator(t, db, generatorState{Done: false, Marker: []byte{}})

	// NewTracker's snapshot.New resumes the in-progress generation, which
	// immediately fails to open the trie at the disk root.
	snaps, err := snapshot.New(snapshot.Config{CacheSize: 16, AsyncBuild: true}, db, tdb, prunedRoot)
	require.NoError(t, err, "snapshot.New() resuming generation at a pruned root")
	t.Cleanup(snaps.Release)

	// Every flatten advances the disk root to the next (equally unresolvable)
	// root and restarts generation against it, exactly as StateDB.Commit's
	// Cap does once per block in production.
	head := flattenCycles(t, snaps, prunedRoot, 32)
	require.Equal(t, head, snaps.DiskRoot(), "disk root after flatten cycles")

	// The wedge: the marker never advances and generation never completes,
	// no matter how many blocks pass...
	gen := readGenerator(t, db)
	require.False(t, gen.Done, "generation Done despite unresolvable disk-root trie")
	require.Empty(t, gen.Marker, "generation marker advanced despite unresolvable disk-root trie")

	// ...so the tree never hands out iterators: this is what the state sync
	// leaf handler's snapshot fast path calls, and why every serving read
	// failed with snapshot_read_error at microsecond latency.
	_, err = snaps.AccountIterator(snaps.DiskRoot(), common.Hash{})
	require.ErrorIs(t, err, snapshot.ErrNotConstructed, "AccountIterator during wedged generation")
	_, err = snaps.StorageIterator(snaps.DiskRoot(), common.Hash{}, common.Hash{})
	require.ErrorIs(t, err, snapshot.ErrNotConstructed, "StorageIterator during wedged generation")
}

func TestSnapshotGenerationCompletesWithRetainedDiskRootTrie(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	cache := state.NewDatabase(db)
	tdb := cache.TrieDB()

	// Identical to the wedged case in every respect but one: the disk root's
	// trie is committed and resolvable.
	sdb, err := state.New(types.EmptyRootHash, cache, nil)
	require.NoError(t, err, "state.New()")
	addr := common.Address{0x01}
	sdb.SetNonce(addr, 1)
	root, err := sdb.Commit(0, false)
	require.NoError(t, err, "state.StateDB.Commit()")
	require.NoError(t, tdb.Commit(root, false), "triedb.Database.Commit()")

	rawdb.WriteSnapshotRoot(db, root)
	writeGenerator(t, db, generatorState{Done: false, Marker: []byte{}})

	snaps, err := snapshot.New(snapshot.Config{CacheSize: 16, AsyncBuild: true}, db, tdb, root)
	require.NoError(t, err, "snapshot.New() resuming generation at a retained root")
	t.Cleanup(snaps.Release)

	require.Eventually(t, func() bool {
		gen, ok := tryReadGenerator(db)
		return ok && gen.Done
	}, 10*time.Second, 10*time.Millisecond, "generation completion with a resolvable disk-root trie")

	it, err := snaps.AccountIterator(snaps.DiskRoot(), common.Hash{})
	require.NoError(t, err, "AccountIterator after completed generation")
	it.Release()

	// Flatten cycles on a completed snapshot don't regress it: generation
	// only restarts while a marker is set.
	flattenCycles(t, snaps, root, 8)
	require.True(t, readGenerator(t, db).Done, "generation Done after flatten cycles")
	it, err = snaps.AccountIterator(snaps.DiskRoot(), common.Hash{})
	require.NoError(t, err, "AccountIterator after flatten cycles on a completed snapshot")
	it.Release()
}
