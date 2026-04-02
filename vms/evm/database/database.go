// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"errors"

	"github.com/ava-labs/libevm/ethdb"

	avalanchegodb "github.com/ava-labs/avalanchego/database"
)

var (
	errSnapshotNotSupported = errors.New("snapshot is not supported")
	errStatNotSupported     = errors.New("stat is not supported")

	_ ethdb.Batch         = (*batch)(nil)
	_ ethdb.KeyValueStore = (*database)(nil)
)

type database struct {
	db avalanchegodb.Database
}

func New(db avalanchegodb.Database) ethdb.KeyValueStore { return database{db} }

func (database) Stat(string) (string, error) { return "", errStatNotSupported }

func (db database) NewBatch() ethdb.Batch { return batch{batch: db.db.NewBatch()} }

func (db database) Has(key []byte) (bool, error) { return db.db.Has(key) }

func (db database) Get(key []byte) ([]byte, error) { return db.db.Get(key) }

func (db database) Put(key, value []byte) error { return db.db.Put(key, value) }

func (db database) Delete(key []byte) error { return db.db.Delete(key) }

func (db database) Compact(start, limit []byte) error { return db.db.Compact(start, limit) }

func (db database) Close() error { return db.db.Close() }

func (db database) NewBatchWithSize(int) ethdb.Batch { return db.NewBatch() }

func (database) NewSnapshot() (ethdb.Snapshot, error) {
	return nil, errSnapshotNotSupported
}

func (db database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	newStart := make([]byte, len(prefix)+len(start))
	copy(newStart, prefix)
	copy(newStart[len(prefix):], start)
	start = newStart

	return db.db.NewIteratorWithStartAndPrefix(start, prefix)
}

type batch struct {
	batch avalanchegodb.Batch
}

func (b batch) Put(key, value []byte) error { return b.batch.Put(key, value) }

func (b batch) Delete(key []byte) error { return b.batch.Delete(key) }

func (b batch) ValueSize() int { return b.batch.Size() }

func (b batch) Write() error { return b.batch.Write() }

func (b batch) Reset() { b.batch.Reset() }

func (b batch) Replay(w ethdb.KeyValueWriter) error { return b.batch.Replay(w) }
