// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie/trienode"
)

// ErrReadOnlyStateDB is returned when committing state opened for reading.
var ErrReadOnlyStateDB = errors.New("read-only state cannot be committed")

var (
	_ state.Database      = readOnlyDatabase{}
	_ state.Trie          = readOnlyTrie{}
	_ ethdb.KeyValueStore = readOnlyDiskDB{}
	_ ethdb.Batch         = discardBatch{}
)

// readOnlyDatabase serves state that can be read and hashed but never
// committed. It wraps any [state.Database], so no scheme is trusted to enforce it.
type readOnlyDatabase struct {
	state.Database
}

func (d readOnlyDatabase) OpenTrie(root common.Hash) (state.Trie, error) {
	tr, err := d.Database.OpenTrie(root)
	if err != nil {
		return nil, err
	}
	return readOnlyTrie{tr}, nil
}

func (d readOnlyDatabase) OpenStorageTrie(stateRoot common.Hash, addr common.Address, root common.Hash, tr state.Trie) (state.Trie, error) {
	// Unwrapped first because a [state.Database] MAY require its own trie here.
	storage, err := d.Database.OpenStorageTrie(stateRoot, addr, root, unwrapTrie(tr))
	if err != nil {
		return nil, err
	}
	return readOnlyTrie{storage}, nil
}

func (d readOnlyDatabase) CopyTrie(tr state.Trie) state.Trie {
	cp := d.Database.CopyTrie(unwrapTrie(tr))
	if cp == nil {
		// A nil copy is meaningful, so it MUST NOT become a non-nil interface
		// holding a nil trie.
		return nil
	}
	return readOnlyTrie{cp}
}

// DiskDB serves reads but discards writes, since [state.StateDB.Commit] flushes
// contract code before the trie can reject the commit. TrieDB is reached only
// after that rejection, so it needs no guard.
func (d readOnlyDatabase) DiskDB() ethdb.KeyValueStore {
	return readOnlyDiskDB{d.Database.DiskDB()}
}

// readOnlyDiskDB drops every write and refuses to close the store it wraps,
// which the rest of the node is still using. Compaction and snapshots pass
// through.
type readOnlyDiskDB struct {
	ethdb.KeyValueStore
}

func (readOnlyDiskDB) Put([]byte, []byte) error         { return nil }
func (readOnlyDiskDB) Delete([]byte) error              { return nil }
func (readOnlyDiskDB) NewBatch() ethdb.Batch            { return discardBatch{} }
func (readOnlyDiskDB) NewBatchWithSize(int) ethdb.Batch { return discardBatch{} }
func (readOnlyDiskDB) Close() error                     { return nil }

// discardBatch drops writes. A zero ValueSize stops the caller attempting a
// flush, and a write cannot error here because rawdb.WriteCode treats one as fatal.
type discardBatch struct{}

func (discardBatch) Put([]byte, []byte) error          { return nil }
func (discardBatch) Delete([]byte) error               { return nil }
func (discardBatch) ValueSize() int                    { return 0 }
func (discardBatch) Write() error                      { return nil }
func (discardBatch) Reset()                            {}
func (discardBatch) Replay(ethdb.KeyValueWriter) error { return nil }

// readOnlyTrie rejects commits and otherwise delegates.
type readOnlyTrie struct {
	state.Trie
}

func (readOnlyTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, ErrReadOnlyStateDB
}

func unwrapTrie(tr state.Trie) state.Trie {
	if ro, ok := tr.(readOnlyTrie); ok {
		return ro.Trie
	}
	return tr
}
