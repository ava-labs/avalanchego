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
func ReadSnapshotBlockHash(db ethdb.KeyValueReader) common.Hash {
	data, _ := db.Get(snapshotBlockHashKey)
	if len(data) != common.HashLength {
		return common.Hash{}
	}
	return common.BytesToHash(data)
}

// WriteSnapshotBlockHash stores the root of the block whose state is contained in
// the persisted snapshot.
func WriteSnapshotBlockHash(db ethdb.KeyValueWriter, blockHash common.Hash) {
	if err := db.Put(snapshotBlockHashKey, blockHash[:]); err != nil {
		log.Crit("Failed to store snapshot block hash", "err", err)
	}
}

// DeleteSnapshotBlockHash deletes the hash of the block whose state is contained in
// the persisted snapshot. Since snapshots are not immutable, this  method can
// be used during updates, so a crash or failure will mark the entire snapshot
// invalid.
func DeleteSnapshotBlockHash(db ethdb.KeyValueWriter) {
	if err := db.Delete(snapshotBlockHashKey); err != nil {
		log.Crit("Failed to remove snapshot block hash", "err", err)
	}
}

// IterateAccountSnapshots returns an iterator for walking all of the accounts in the snapshot
func IterateAccountSnapshots(db ethdb.Iteratee) ethdb.Iterator {
	it := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
	keyLen := len(rawdb.SnapshotAccountPrefix) + common.HashLength
	return rawdb.NewKeyLengthIterator(it, keyLen)
}
