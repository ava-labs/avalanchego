// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"bytes"
	"slices"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"
)

// SnapshotTree is a real [snapshot.Tree] that logs the iterators it serves.
type SnapshotTree struct {
	snapshotReads

	tree *snapshot.Tree
}

// NewSnapshotTree builds a real [snapshot.Tree] with its disk layer generated
// from the state at root, for driving a handler through the production type.
func NewSnapshotTree(t *testing.T, disk ethdb.Database, trieDB *triedb.Database, root common.Hash) *SnapshotTree {
	t.Helper()
	tree, err := snapshot.New(snapshot.Config{CacheSize: 1}, disk, trieDB, root)
	require.NoError(t, err)
	require.Equal(t, root, tree.DiskRoot())
	return &SnapshotTree{tree: tree}
}

func (s *SnapshotTree) DiskRoot() common.Hash { return s.tree.DiskRoot() }

func (s *SnapshotTree) AccountIterator(root, seek common.Hash) (snapshot.AccountIterator, error) {
	s.record(SnapshotRead{Root: root})
	return s.tree.AccountIterator(root, seek)
}

func (s *SnapshotTree) StorageIterator(root, account, seek common.Hash) (snapshot.StorageIterator, error) {
	s.record(SnapshotRead{Root: root, Account: account})
	return s.tree.StorageIterator(root, account, seek)
}

// RequireRootRetired asserts tree cannot serve root. libevm reports the miss
// with no sentinel, so this asserts iteration is impossible, not which error.
func RequireRootRetired(t *testing.T, tree *SnapshotTree, root common.Hash) {
	t.Helper()
	// The inner tree, so probing during setup stays out of the read log.
	it, err := tree.tree.AccountIterator(root, common.Hash{})
	if err == nil {
		it.Release()
		t.Fatalf("snapshot tree still serves root %s", root)
	}
}

// StaticPair is one key/value entry.
type StaticPair struct {
	K, V []byte
}

// StaticSnapshot is an in-memory [evmstate.SnapshotReader] for tests. Accounts
// and each Storage entry are sorted by K, accounts holding slim values. The root
// is ignored, as a real disk layer serves whatever it last flushed.
type StaticSnapshot struct {
	snapshotReads

	Accounts []StaticPair
	Storage  map[common.Hash][]StaticPair
	Err      error
}

// DiskRoot is the zero hash, never a valid requested root, so a test can tell a
// disk read from a root-scoped one.
func (*StaticSnapshot) DiskRoot() common.Hash { return common.Hash{} }

func (s *StaticSnapshot) AccountIterator(root, seek common.Hash) (snapshot.AccountIterator, error) {
	s.record(SnapshotRead{Root: root})
	if s.Err != nil {
		return nil, s.Err
	}
	return &staticAccountIter{pairs: seekPairs(s.Accounts, seek), idx: -1}, nil
}

func (s *StaticSnapshot) StorageIterator(root, account, seek common.Hash) (snapshot.StorageIterator, error) {
	s.record(SnapshotRead{Root: root, Account: account})
	if s.Err != nil {
		return nil, s.Err
	}
	return &staticStorageIter{pairs: seekPairs(s.Storage[account], seek), idx: -1}, nil
}

// seekPairs drops the entries ordered before seek.
func seekPairs(pairs []StaticPair, seek common.Hash) []StaticPair {
	i := 0
	for i < len(pairs) && bytes.Compare(pairs[i].K, seek.Bytes()) < 0 {
		i++
	}
	return pairs[i:]
}

type staticAccountIter struct {
	pairs []StaticPair
	idx   int
}

func (it *staticAccountIter) Next() bool {
	it.idx++
	return it.idx < len(it.pairs)
}

func (it *staticAccountIter) Hash() common.Hash {
	if it.idx < 0 || it.idx >= len(it.pairs) {
		return common.Hash{}
	}
	return common.BytesToHash(it.pairs[it.idx].K)
}

func (it *staticAccountIter) Account() []byte { return it.pairs[it.idx].V }
func (*staticAccountIter) Error() error       { return nil }
func (*staticAccountIter) Release()           {}

type staticStorageIter struct {
	pairs []StaticPair
	idx   int
}

func (it *staticStorageIter) Next() bool {
	it.idx++
	return it.idx < len(it.pairs)
}

func (it *staticStorageIter) Hash() common.Hash {
	if it.idx < 0 || it.idx >= len(it.pairs) {
		return common.Hash{}
	}
	return common.BytesToHash(it.pairs[it.idx].K)
}

func (it *staticStorageIter) Slot() []byte { return it.pairs[it.idx].V }
func (*staticStorageIter) Error() error    { return nil }
func (*staticStorageIter) Release()        {}

// SnapshotRead is one iterator a handler opened.
type SnapshotRead struct {
	Root    common.Hash
	Account common.Hash // zero for the account trie
}

// snapshotReads is the read log every snapshot source here keeps, so a test can
// tell a snapshot read from a trie fallback serving the same leaves.
type snapshotReads struct {
	mu    sync.Mutex
	reads []SnapshotRead
}

// Reads returns every iterator opened, naming the layer each read.
func (s *snapshotReads) Reads() []SnapshotRead {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.reads)
}

// One responder serves peers concurrently, so the log is guarded.
func (s *snapshotReads) record(read SnapshotRead) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reads = append(s.reads, read)
}
