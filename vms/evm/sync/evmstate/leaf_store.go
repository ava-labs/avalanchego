// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
)

var errDecodeAccount = errors.New("could not decode account leaf")

// codeEnqueuer enqueues contract-code hashes discovered while syncing the
// account trie. Satisfied by [code.Queue].
type codeEnqueuer interface {
	AddCode(ctx context.Context, hashes []common.Hash) error
}

// storageRegistry records storage tries that must be synced for an account.
// Satisfied by [trieQueue].
type storageRegistry interface {
	RegisterStorageTrie(root, account common.Hash) error
}

// leafStore is the seam the segmented reconstruction uses: it writes
// each verified batch to the passed segment batch (a snapshot write) and later
// re-reads those leaves from the snapshot in key order to feed the StackTrie.
type leafStore interface {
	writeLeaves(ctx context.Context, db ethdb.KeyValueWriter, keys, vals [][]byte) error
	iterateLeaves(seek common.Hash) ethdb.Iterator
}

// accountLeaves decodes account leaves, writes each account snapshot, registers
// non-empty storage tries for a later sync, and enqueues non-empty code hashes.
type accountLeaves struct {
	db        ethdb.KeyValueStore
	codeQueue codeEnqueuer
	trieQueue storageRegistry
}

func newAccountLeaves(db ethdb.KeyValueStore, codeQueue codeEnqueuer, trieQueue storageRegistry) *accountLeaves {
	return &accountLeaves{
		db:        db,
		codeQueue: codeQueue,
		trieQueue: trieQueue,
	}
}

// writeLeaves decodes accounts and writes their snapshots to db, discovering
// storage tries and code as it goes. Batch capping is the segment's job.
func (s *accountLeaves) writeLeaves(ctx context.Context, db ethdb.KeyValueWriter, keys, vals [][]byte) error {
	var codeHashes []common.Hash
	for i, key := range keys {
		accountHash := common.BytesToHash(key)
		var acc types.StateAccount
		if err := rlp.DecodeBytes(vals[i], &acc); err != nil {
			return fmt.Errorf("%w %s (len %d): %w", errDecodeAccount, accountHash, len(vals[i]), err)
		}

		writeAccountSnapshot(db, accountHash, acc)

		if acc.Root != (common.Hash{}) && acc.Root != types.EmptyRootHash {
			if err := s.trieQueue.RegisterStorageTrie(acc.Root, accountHash); err != nil {
				return err
			}
		}

		codeHash := common.BytesToHash(acc.CodeHash)
		if codeHash != (common.Hash{}) && codeHash != types.EmptyCodeHash {
			codeHashes = append(codeHashes, codeHash)
		}
	}
	return s.codeQueue.AddCode(ctx, codeHashes)
}

// iterateLeaves re-reads the account snapshot from seek as full-RLP trie leaves.
func (s *accountLeaves) iterateLeaves(seek common.Hash) ethdb.Iterator {
	return newAccountLeafIterator(s.db, seek)
}

// flushIfFull writes and resets batch once it grows past [ethdb.IdealBatchSize],
// capping memory during a long walk.
func flushIfFull(batch ethdb.Batch) error {
	if batch.ValueSize() <= ethdb.IdealBatchSize {
		return nil
	}
	if err := batch.Write(); err != nil {
		return err
	}
	batch.Reset()
	return nil
}

// writeAccountSnapshot stores acc to the snapshot at accHash in SlimAccountRLP
// form, omitting empty code and storage.
func writeAccountSnapshot(db ethdb.KeyValueWriter, accHash common.Hash, acc types.StateAccount) {
	rawdb.WriteAccountSnapshot(db, accHash, types.SlimAccountRLP(acc))
}

// storageLeaves writes each verified storage leaf to the storage snapshot of
// every account that shares this trie's root.
type storageLeaves struct {
	db       ethdb.KeyValueStore
	accounts []common.Hash
}

func newStorageLeaves(db ethdb.KeyValueStore, accounts []common.Hash) *storageLeaves {
	return &storageLeaves{
		db:       db,
		accounts: accounts,
	}
}

// writeLeaves writes each storage leaf to db for every account sharing the root.
// Batch capping is the segment's job.
func (s *storageLeaves) writeLeaves(ctx context.Context, db ethdb.KeyValueWriter, keys, vals [][]byte) error {
	for _, account := range s.accounts {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, key := range keys {
			rawdb.WriteStorageSnapshot(db, account, common.BytesToHash(key), vals[i])
		}
	}
	return nil
}

// iterateLeaves re-reads the storage snapshot from seek. All accounts sharing the
// root hold identical storage, so the first account's snapshot reconstructs it.
func (s *storageLeaves) iterateLeaves(seek common.Hash) ethdb.Iterator {
	return newStorageLeafIterator(s.db, s.accounts[0], seek)
}

// newAccountLeafIterator iterates the account snapshot from seek, yielding the
// account hash as the trie key and the full-RLP account as the value. The
// snapshot stores slim accounts, so each value is expanded on read.
func newAccountLeafIterator(db ethdb.Iteratee, seek common.Hash) *accountLeafIterator {
	inner := rawdb.NewKeyLengthIterator(
		db.NewIterator(rawdb.SnapshotAccountPrefix, seek.Bytes()),
		len(rawdb.SnapshotAccountPrefix)+common.HashLength,
	)
	return &accountLeafIterator{Iterator: inner}
}

type accountLeafIterator struct {
	ethdb.Iterator
	val []byte
	err error
}

func (it *accountLeafIterator) Next() bool {
	if it.err != nil {
		return false
	}
	if !it.Iterator.Next() {
		it.val = nil
		return false
	}
	it.val, it.err = types.FullAccountRLP(it.Iterator.Value())
	return it.err == nil
}

func (it *accountLeafIterator) Key() []byte {
	return it.Iterator.Key()[len(rawdb.SnapshotAccountPrefix):]
}

func (it *accountLeafIterator) Value() []byte { return it.val }

func (it *accountLeafIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.Iterator.Error()
}

// newStorageLeafIterator iterates the storage snapshot of account from seek,
// yielding the storage hash as the trie key and the slot value unchanged.
func newStorageLeafIterator(db ethdb.Iteratee, account, seek common.Hash) *storageLeafIterator {
	prefix := append(append([]byte{}, rawdb.SnapshotStoragePrefix...), account.Bytes()...)
	inner := rawdb.NewKeyLengthIterator(
		db.NewIterator(prefix, seek.Bytes()),
		len(rawdb.SnapshotStoragePrefix)+2*common.HashLength,
	)
	return &storageLeafIterator{Iterator: inner, prefixLen: len(prefix)}
}

type storageLeafIterator struct {
	ethdb.Iterator
	prefixLen int
}

func (it *storageLeafIterator) Key() []byte {
	return it.Iterator.Key()[it.prefixLen:]
}
