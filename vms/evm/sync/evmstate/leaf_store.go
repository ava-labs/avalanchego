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

// codeEnqueuer takes the code hashes discovered while syncing. Satisfied by [code.Syncer].
type codeEnqueuer interface {
	AddCode(ctx context.Context, hashes []common.Hash) error
}

// storageRegistry records storage tries that must be synced for an account.
// Satisfied by [trieQueue].
type storageRegistry interface {
	RegisterStorageTrie(root, account common.Hash) error
}

// leafStore is the seam the reconstruction writes leaves through, and reads them back
// from in key order. What a leaf means differs for accounts and storage.
type leafStore interface {
	writeLeaves(ctx context.Context, db ethdb.KeyValueWriter, batch leafBatch) error
	iterateLeaves(seek common.Hash) ethdb.Iterator
}

// accountLeaves writes each account's snapshot and discovers its storage trie and code.
type accountLeaves struct {
	db         ethdb.KeyValueStore
	codeSyncer codeEnqueuer
	trieQueue  storageRegistry
}

func newAccountLeaves(db ethdb.KeyValueStore, codeSyncer codeEnqueuer, trieQueue storageRegistry) *accountLeaves {
	return &accountLeaves{
		db:         db,
		codeSyncer: codeSyncer,
		trieQueue:  trieQueue,
	}
}

// writeLeaves writes account snapshots, discovering storage tries and code as it goes.
// Batch capping is the segment's job.
func (s *accountLeaves) writeLeaves(ctx context.Context, db ethdb.KeyValueWriter, batch leafBatch) error {
	var codeHashes []common.Hash
	for i, key := range batch.keys {
		accountHash := common.BytesToHash(key)
		var acc types.StateAccount
		if err := rlp.DecodeBytes(batch.vals[i], &acc); err != nil {
			return fmt.Errorf("%w %s (len %d): %w", errDecodeAccount, accountHash, len(batch.vals[i]), err)
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
	return s.codeSyncer.AddCode(ctx, codeHashes)
}

// iterateLeaves re-reads the account snapshot from seek as full-RLP trie leaves.
func (s *accountLeaves) iterateLeaves(seek common.Hash) ethdb.Iterator {
	return newAccountLeafIterator(s.db, seek)
}

// writeAccountSnapshot stores acc in slim form, omitting empty code and storage.
func writeAccountSnapshot(db ethdb.KeyValueWriter, accHash common.Hash, acc types.StateAccount) {
	rawdb.WriteAccountSnapshot(db, accHash, types.SlimAccountRLP(acc))
}

// storageLeaves writes each leaf to every account sharing this trie's root.
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

// writeLeaves writes each leaf once per sharing account. Batch capping is the
// segment's job.
func (s *storageLeaves) writeLeaves(ctx context.Context, db ethdb.KeyValueWriter, batch leafBatch) error {
	for _, account := range s.accounts {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, key := range batch.keys {
			rawdb.WriteStorageSnapshot(db, account, common.BytesToHash(key), batch.vals[i])
		}
	}
	return nil
}

// iterateLeaves re-reads the storage snapshot from seek. All accounts sharing the
// root hold identical storage, so the first account's snapshot reconstructs it.
func (s *storageLeaves) iterateLeaves(seek common.Hash) ethdb.Iterator {
	return newStorageLeafIterator(s.db, s.accounts[0], seek)
}

// newAccountLeafIterator yields the account hash as the trie key and the full-RLP
// account as the value. The snapshot stores slim accounts, so values expand on read.
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

// newStorageLeafIterator yields the slot hash as the trie key and the value unchanged.
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
