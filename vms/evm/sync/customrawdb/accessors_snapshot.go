// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

// ReadSnapshotBlockHash retrieves the hash of the block whose state is contained in
// the persisted snapshot.
func ReadSnapshotBlockHash(db ethdb.KeyValueReader) (common.Hash, error) {
	ok, err := db.Has(snapshotBlockHashKey)
	if err != nil {
		return common.Hash{}, err
	}
	if !ok {
		return common.Hash{}, ErrEntryNotFound
	}

	data, err := db.Get(snapshotBlockHashKey)
	if err != nil {
		return common.Hash{}, err
	}
	if len(data) != common.HashLength {
		return common.Hash{}, ErrInvalidData
	}
	return common.BytesToHash(data), nil
}

// WriteSnapshotBlockHash stores the root of the block whose state is contained in
// the persisted snapshot.
func WriteSnapshotBlockHash(db ethdb.KeyValueWriter, blockHash common.Hash) error {
	if err := db.Put(snapshotBlockHashKey, blockHash[:]); err != nil {
		log.Error("Failed to store snapshot block hash", "err", err)
		return err
	}
	return nil
}

// DeleteSnapshotBlockHash deletes the hash of the block whose state is contained in
// the persisted snapshot. Since snapshots are not immutable, this  method can
// be used during updates, so a crash or failure will mark the entire snapshot
// invalid.
func DeleteSnapshotBlockHash(db ethdb.KeyValueWriter) error {
	if err := db.Delete(snapshotBlockHashKey); err != nil {
		log.Error("Failed to remove snapshot block hash", "err", err)
		return err
	}
	return nil
}

// NewAccountSnapshotsIterator returns an iterator for walking all of the accounts in the snapshot
func NewAccountSnapshotsIterator(db ethdb.Iteratee) ethdb.Iterator {
	it := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
	keyLen := len(rawdb.SnapshotAccountPrefix) + common.HashLength
	return rawdb.NewKeyLengthIterator(it, keyLen)
}
