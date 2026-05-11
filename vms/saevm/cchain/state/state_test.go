// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Tests for the state package. Two kinds:
//
//   - Differential tests that assert byte-for-byte equivalence between the
//     new state package and the legacy graft/coreth/plugin/evm/atomic/state
//     package. The migration to the new code does not run a database
//     rewrite, so the indices retained by the new code (atomicTxDB,
//     atomicHeightTxDB, atomicTrieDB, atomicTrieMetaDB) must produce the
//     same on-disk bytes as the old code given the same inputs.
//   - API tests that exercise [State]'s public methods directly.

package state

import (
	"bytes"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type block struct {
	height uint64
	txs    []*tx.Tx
}

// newTx returns an Import tx whose canonical bytes — and therefore tx ID —
// are determined by outputIndex. All other fields are zero. Just enough
// structure for the codec, ID derivation, and per-chain merging in
// [applyTrie] to work; no semantic validity is asserted or needed by these
// tests.
func newTx(outputIndex uint32) *tx.Tx {
	return &tx.Tx{
		Unsigned: &tx.Import{
			ImportedInputs: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{OutputIndex: outputIndex},
				In:     &secp256k1fx.TransferInput{},
			}},
		},
	}
}

func newState(t *testing.T) *State {
	t.Helper()
	s, err := New(memdb.New())
	require.NoError(t, err)
	return s
}

// blockSequence covers single-tx and multi-tx blocks at low heights, then
// hits each commit boundary directly so the trie commit and on-disk
// metadata fire without a catch-up. Non-boundary heights can skip; boundary
// heights themselves are part of the sequence.
func blockSequence(t *testing.T) []block {
	t.Helper()
	return []block{
		{1, nil}, // empty Apply: should be a no-op for both tx index and trie.
		{2, []*tx.Tx{newTx(1)}},
		{3, []*tx.Tx{newTx(2)}},
		{5, []*tx.Tx{newTx(3), newTx(4)}},
		{7, []*tx.Tx{newTx(5)}},
		{8, []*tx.Tx{newTx(6)}},
		{10, []*tx.Tx{newTx(7), newTx(8)}},
		{12, []*tx.Tx{newTx(9)}},
		{commitInterval, []*tx.Tx{newTx(10)}},
		{commitInterval + 2, []*tx.Tx{newTx(11)}},
		{2 * commitInterval, []*tx.Tx{newTx(12)}},
		{2*commitInterval + 1, []*tx.Tx{newTx(13)}},
	}
}

// ===========================================================================
// Differential tests
// ===========================================================================

// TestStateDifferentialMatchesOldCoreth drives the same sequence of
// (height, txs) blocks through both the new state package and the old
// coreth atomic state package, then asserts every byte under each retained
// prefix is equal between the two databases.
func TestStateDifferentialMatchesOldCoreth(t *testing.T) {
	newDB, oldUnderlying, oldVDB, newSt, oldDriver := newPair(t)
	for _, b := range blockSequence(t) {
		applyToBoth(t, newSt, oldDriver, oldVDB, b)
		assertPrefixesEqual(t, newDB, oldUnderlying, b.height)
	}
}

// TestStateDifferentialReplayMatchesOldCoreth applies a sequence, drops
// the in-memory state on the new side (re-opens via New), then
// continues applying. Verifies that replay reconstructs the in-memory tip
// such that subsequent Apply calls remain byte-equivalent to the old code.
func TestStateDifferentialReplayMatchesOldCoreth(t *testing.T) {
	newDB, oldUnderlying, oldVDB, newSt, oldDriver := newPair(t)

	seq := blockSequence(t)
	splitAt := len(seq) / 2

	for _, b := range seq[:splitAt] {
		applyToBoth(t, newSt, oldDriver, oldVDB, b)
	}
	assertPrefixesEqual(t, newDB, oldUnderlying, seq[splitAt-1].height)

	// Re-open the new state. The old AtomicBackend is still in-memory; the
	// new one is reconstructed only from disk via replay.
	reopened, err := New(newDB)
	require.NoError(t, err)

	for _, b := range seq[splitAt:] {
		applyToBoth(t, reopened, oldDriver, oldVDB, b)
		assertPrefixesEqual(t, newDB, oldUnderlying, b.height)
	}
}

// oldDriver wraps the old AtomicBackend / AtomicRepository so the test can
// drive the equivalent operations of the new state.Apply.
type oldDriver struct {
	repo      *state.AtomicRepository
	backend   *state.AtomicBackend
	parent    common.Hash
	heightCnt uint64
}

func newPair(t *testing.T) (newDB database.Database, oldUnderlying database.Database, oldVDB *versiondb.Database, newSt *State, drv *oldDriver) {
	t.Helper()
	newDB = memdb.New()
	oldUnderlying = memdb.New()
	oldVDB = versiondb.New(oldUnderlying)

	newSt, err := New(newDB)
	require.NoError(t, err)

	repo, err := state.NewAtomicTxRepository(oldVDB, atomic.Codec, 0)
	require.NoError(t, err)
	backend, err := state.NewAtomicBackend(nil, nil, repo, 0, common.Hash{}, commitInterval)
	require.NoError(t, err)

	drv = &oldDriver{repo: repo, backend: backend}
	return
}

func applyToBoth(t *testing.T, newSt *State, drv *oldDriver, oldVDB *versiondb.Database, b block) {
	t.Helper()

	// New side: single Apply does everything.
	require.NoError(t, newSt.Apply(b.height, b.txs))

	// Old side: convert txs, drive InsertTxs + Repo.Write + AcceptTrie,
	// then commit the versiondb.
	oldTxs := convertNewToOld(t, b.txs)

	// Generate a unique block hash deterministically. drv.heightCnt is used
	// instead of b.height so non-sequential heights still get unique hashes
	// (height bytes alone could collide if a future test reuses heights).
	drv.heightCnt++
	var blockHash common.Hash
	blockHash[0] = byte(drv.heightCnt >> 8)
	blockHash[1] = byte(drv.heightCnt)
	blockHash[31] = byte(b.height)

	root, err := drv.backend.InsertTxs(blockHash, b.height, drv.parent, oldTxs)
	require.NoError(t, err)

	require.NoError(t, drv.repo.Write(b.height, oldTxs))
	_, err = drv.backend.AtomicTrie().AcceptTrie(b.height, root)
	require.NoError(t, err)
	drv.backend.SetLastAccepted(blockHash)
	drv.parent = blockHash

	require.NoError(t, oldVDB.Commit())
}

// retainedPrefixes are the prefixes whose contents must remain
// byte-identical between old and new. They correspond to the four prefixdb
// roots defined in old code at atomic_repository.go:30-34, minus
// atomicRepoMetadataDB which the new code drops.
var retainedPrefixes = []string{
	"atomicTxDB",
	"atomicHeightTxDB",
	"atomicTrieDB",
}

func assertPrefixesEqual(t *testing.T, newDB, oldDB database.Database, height uint64) {
	t.Helper()

	for _, name := range retainedPrefixes {
		newPDB := prefixdb.New([]byte(name), newDB)
		oldPDB := prefixdb.New([]byte(name), oldDB)
		assertDBsEqual(t, newPDB, oldPDB, name, height, nil)
	}

	// atomicTrieMetaDB: the new code adds atomicTrieLastAppliedBlock under
	// this prefix; the old code does not. Filter it out before comparing.
	appliedKey := []byte("atomicTrieLastAppliedBlock")
	newMeta := prefixdb.New([]byte("atomicTrieMetaDB"), newDB)
	oldMeta := prefixdb.New([]byte("atomicTrieMetaDB"), oldDB)
	assertDBsEqual(t, newMeta, oldMeta, "atomicTrieMetaDB", height, func(k []byte) bool {
		return bytes.Equal(k, appliedKey)
	})
}

// assertDBsEqual iterates a and b in lockstep and asserts every (key,value)
// matches. If skipA is non-nil, entries on the a side whose key satisfies
// the predicate are skipped before comparison.
func assertDBsEqual(t *testing.T, a, b database.Iteratee, prefix string, height uint64, skipA func([]byte) bool) {
	t.Helper()

	itA := a.NewIterator()
	defer itA.Release()
	itB := b.NewIterator()
	defer itB.Release()

	advanceA := func() bool {
		for itA.Next() {
			if skipA == nil || !skipA(itA.Key()) {
				return true
			}
		}
		return false
	}

	for {
		hasA := advanceA()
		hasB := itB.Next()
		if !hasA && !hasB {
			break
		}

		require.Equalf(t, hasA, hasB, "prefix=%s height=%d: iterator length mismatch", prefix, height)
		require.Equalf(t, itB.Key(), itA.Key(), "prefix=%s height=%d: key mismatch", prefix, height)
		require.Equalf(t, itB.Value(), itA.Value(), "prefix=%s height=%d key=%x: value mismatch", prefix, height, itA.Key())
	}
	require.NoError(t, itA.Error())
	require.NoError(t, itB.Error())
}

// convertNewToOld serializes each new tx and parses the bytes through the
// old codec to produce an equivalent old atomic.Tx. The codec registration
// order matches between the two packages, so this round-trip preserves
// bytes.
func convertNewToOld(t *testing.T, newTxs []*tx.Tx) []*atomic.Tx {
	t.Helper()
	out := make([]*atomic.Tx, len(newTxs))
	for i, n := range newTxs {
		b, err := n.Bytes()
		require.NoError(t, err)
		oldTx, err := atomic.ExtractAtomicTx(b, atomic.Codec)
		require.NoError(t, err)
		out[i] = oldTx
	}
	return out
}

// ===========================================================================
// API tests
// ===========================================================================

func TestNew_EmptyDB(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	require.Equal(uint64(0), s.LastCommitted())

	root, err := s.GetRoot(0)
	require.NoError(err)
	require.Equal(types.EmptyRootHash, root)
}

// TestApply table covers the Apply→{GetTx, GetRoot, LastCommitted} surface.
// Every row asserts the same universal post-conditions, so individual rows
// only need to describe the input sequence and the expected
// last-committed height.
func TestApply(t *testing.T) {
	cases := []struct {
		name              string
		blocks            []block
		wantLastCommitted uint64
	}{
		{
			name: "empty applies at non-boundary heights",
			blocks: []block{
				{height: 1, txs: nil},
				{height: 2, txs: []*tx.Tx{}},
			},
			wantLastCommitted: 0,
		},
		{
			name: "single tx at non-boundary height",
			blocks: []block{
				{height: 5, txs: []*tx.Tx{newTx(1)}},
			},
			wantLastCommitted: 0,
		},
		{
			name: "multiple txs at single height",
			blocks: []block{
				{height: 5, txs: []*tx.Tx{newTx(1), newTx(2), newTx(3)}},
			},
			wantLastCommitted: 0,
		},
		{
			name: "multiple non-boundary heights",
			blocks: []block{
				{height: 2, txs: []*tx.Tx{newTx(1)}},
				{height: 3, txs: []*tx.Tx{newTx(2), newTx(3)}},
				{height: 5, txs: []*tx.Tx{newTx(4)}},
			},
			wantLastCommitted: 0,
		},
		{
			name: "crosses a single commit boundary",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{newTx(1)}},
				{height: commitInterval, txs: []*tx.Tx{newTx(2)}},
				{height: commitInterval + 1, txs: []*tx.Tx{newTx(3)}},
			},
			wantLastCommitted: commitInterval,
		},
		{
			name: "crosses two commit boundaries",
			blocks: []block{
				{height: commitInterval, txs: []*tx.Tx{newTx(1)}},
				{height: 2 * commitInterval, txs: []*tx.Tx{newTx(2)}},
			},
			wantLastCommitted: 2 * commitInterval,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)
			s := newState(t)

			// Snapshot inputs so the universal "Apply doesn't mutate the
			// caller's slice" invariant can be checked at the end.
			snapshots := make([][]*tx.Tx, len(tc.blocks))
			for i, b := range tc.blocks {
				snapshots[i] = slices.Clone(b.txs)
			}

			for _, b := range tc.blocks {
				require.NoError(s.Apply(b.height, b.txs))
			}

			require.Equal(tc.wantLastCommitted, s.LastCommitted())

			for i, b := range tc.blocks {
				require.Equal(snapshots[i], b.txs, "Apply mutated input slice at block %d", i)
			}

			for _, b := range tc.blocks {
				for _, want := range b.txs {
					got, height, err := s.GetTx(want.ID())
					require.NoError(err)
					require.Equal(b.height, height)
					require.Empty(cmp.Diff(want, got, txtest.CmpOpt()))
				}

				_, err := s.GetRoot(b.height)
				if b.height%commitInterval == 0 {
					require.NoError(err, "GetRoot at commit boundary %d", b.height)
				} else {
					require.ErrorIs(err, database.ErrNotFound, "GetRoot at non-boundary %d", b.height)
				}
			}

			root, err := s.GetRoot(0)
			require.NoError(err)
			require.Equal(types.EmptyRootHash, root)

			_, _, err = s.GetTx(ids.ID{0xDE, 0xAD, 0xBE, 0xEF})
			require.ErrorIs(err, database.ErrNotFound)
		})
	}
}

// TestApply_SortInvariant verifies that the order of txs in the input slice
// does not affect the on-disk trie root. The txs all target the same source
// chain (zero), so without the sort in Apply, the per-chain Requests merge
// in [applyTrie] would produce different trie value bytes — and therefore a
// different root — depending on the input order.
func TestApply_SortInvariant(t *testing.T) {
	require := require.New(t)

	txs := []*tx.Tx{
		newTx(1),
		newTx(2),
		newTx(3),
	}

	apply := func(in []*tx.Tx) common.Hash {
		s := newState(t)
		require.NoError(s.Apply(commitInterval, in))
		root, err := s.GetRoot(commitInterval)
		require.NoError(err)
		return root
	}

	forward := apply(txs)
	reversed := apply([]*tx.Tx{txs[2], txs[1], txs[0]})
	rotated := apply([]*tx.Tx{txs[1], txs[2], txs[0]})

	require.Equal(forward, reversed)
	require.Equal(forward, rotated)
	require.NotEqual(types.EmptyRootHash, forward)
}

// TestNew_ReplayPreservesState applies the full block sequence, discards
// the in-memory state, re-opens via New, and verifies that the rebuilt
// state matches what was in memory before the close and that subsequent
// Apply calls extend the same trie tip.
func TestNew_ReplayPreservesState(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s, err := New(db)
	require.NoError(err)

	seq := blockSequence(t)
	for _, b := range seq {
		require.NoError(s.Apply(b.height, b.txs))
	}
	wantLastCommitted := s.LastCommitted()
	wantRoot := s.currentRoot

	reopened, err := New(db)
	require.NoError(err)

	require.Equal(wantLastCommitted, reopened.LastCommitted())
	require.Equal(wantRoot, reopened.currentRoot)

	for _, b := range seq {
		for _, want := range b.txs {
			got, height, err := reopened.GetTx(want.ID())
			require.NoError(err)
			require.Equal(b.height, height)
			require.Empty(cmp.Diff(want, got, txtest.CmpOpt()))
		}
	}

	// A subsequent Apply on the reopened state must extend the same trie
	// tip: applying at the next commit boundary advances the last-committed
	// marker and the new tx is retrievable.
	extra := newTx(99)
	nextBoundary := (wantLastCommitted/commitInterval + 1) * commitInterval
	require.NoError(reopened.Apply(nextBoundary, []*tx.Tx{extra}))
	require.Equal(nextBoundary, reopened.LastCommitted())

	gotExtra, h, err := reopened.GetTx(extra.ID())
	require.NoError(err)
	require.Equal(nextBoundary, h)
	require.Empty(cmp.Diff(extra, gotExtra, txtest.CmpOpt()))
}
