// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
)

// RegisterServer registers the handlers for the state sync protocol.
//
// TODO(alarso16): Find a way to wire through Firewood.
func (h *SummaryHandler) RegisterServer(tdb *triedb.Database, snap *snapshot.Tree) error {
	var (
		log    = h.snowCtx.Log
		p2pNet = h.network.Network
		db     = h.db
	)
	if err := block.RegisterHandler(log, p2pNet, db, h.reg); err != nil {
		return fmt.Errorf("registering block handler: %w", err)
	}

	if err := hashdb.RegisterHandler(
		log, p2pNet,
		p2p.EVMLeafRequestHandlerID,
		tdb,
		common.HashLength,
		h.reg,
		hashdbOptions(h.db, snap)...,
	); err != nil {
		return fmt.Errorf("registering hashdb handler: %w", err)
	}

	if err := code.RegisterHandler(log, p2pNet, db, h.reg); err != nil {
		return fmt.Errorf("registering code handler: %w", err)
	}

	return nil
}

// hashdbOptions enables the snapshot fast path only when the node maintains a
// snapshot at all: without one the disk entries are absent or arbitrarily
// stale, so attempting them would only add failed segment proofs on top of the
// trie reads that must happen anyway.
func hashdbOptions(db ethdb.Iteratee, snap *snapshot.Tree) []hashdb.HandlerOption {
	if snap == nil {
		return nil
	}
	return []hashdb.HandlerOption{
		hashdb.WithSnapshot(&syncSnap{db: db}),
	}
}

var _ hashdb.Snapshot = (*syncSnap)(nil)

// syncSnap serves leaf requests from the snapshot entries on disk, read
// directly from the database rather than through [snapshot.Tree]. The tree's
// iterators refuse to run while the snapshot is generating (returning
// [snapshot.ErrNotConstructed]) — which is exactly the state of a
// recently-synced node serving other syncers — and can be invalidated
// mid-iteration when execution flattens layers via [snapshot.Tree.Cap]. Raw
// disk reads always succeed; partially generated or stale entries are safe to
// serve because the [hashdb.Snapshot] contract guarantees nothing about the
// state served — the handler proves every range against the trie and fills
// the gaps from the trie itself. This mirrors coreth's DiskAccountIterator
// and DiskStorageIterator serving path.
type syncSnap struct {
	db ethdb.Iteratee
}

// AccountIterator implements [hashdb.Snapshot].
func (s *syncSnap) AccountIterator(start common.Hash) (snapshot.AccountIterator, error) {
	return &diskAccountIterator{newDiskIterator(s.db, rawdb.SnapshotAccountPrefix, start)}, nil
}

// StorageIterator implements [hashdb.Snapshot].
func (s *syncSnap) StorageIterator(account common.Hash, start common.Hash) (snapshot.StorageIterator, error) {
	prefix := slices.Concat(rawdb.SnapshotStoragePrefix, account.Bytes())
	return &diskStorageIterator{newDiskIterator(s.db, prefix, start)}, nil
}

// diskIterator walks one trie's snapshot entries in key order, directly from
// the database. Keys under the same prefix with a different length are
// skipped: the single-byte snapshot prefixes are shared with unrelated keys,
// e.g. trie nodes. The hash and value are copied out of the underlying
// iterator because [hashdb.Snapshot] requires them to survive later calls to
// Next and Release.
type diskIterator struct {
	it     ethdb.Iterator
	keyLen int
	hash   common.Hash
	value  []byte
}

func newDiskIterator(db ethdb.Iteratee, prefix []byte, start common.Hash) *diskIterator {
	return &diskIterator{
		it:     db.NewIterator(prefix, start.Bytes()),
		keyLen: len(prefix) + common.HashLength,
	}
}

// Next implements [snapshot.Iterator].
func (d *diskIterator) Next() bool {
	for d.it.Next() {
		key := d.it.Key()
		if len(key) != d.keyLen {
			continue
		}
		d.hash = common.BytesToHash(key[d.keyLen-common.HashLength:])
		d.value = slices.Clone(d.it.Value())
		return true
	}
	return false
}

// Error implements [snapshot.Iterator].
func (d *diskIterator) Error() error {
	return d.it.Error()
}

// Hash implements [snapshot.Iterator].
func (d *diskIterator) Hash() common.Hash {
	return d.hash
}

// Release implements [snapshot.Iterator].
func (d *diskIterator) Release() {
	d.it.Release()
}

type diskAccountIterator struct{ *diskIterator }

// Account returns the RLP-encoded slim account at the cursor, the encoding
// [hashdb] expects from [snapshot.AccountIterator].
func (d *diskAccountIterator) Account() []byte {
	return d.value
}

type diskStorageIterator struct{ *diskIterator }

// Slot returns the storage slot value at the cursor.
func (d *diskStorageIterator) Slot() []byte {
	return d.value
}
