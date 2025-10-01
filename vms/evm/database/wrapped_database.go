// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"errors"

	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
)

var (
	errSnapshotNotSupported = errors.New("snapshot is not supported")
	errStatNotSupported     = errors.New("stat is not supported")

	_ ethdb.Batch         = (*ethBatchWrapper)(nil)
	_ ethdb.KeyValueStore = (*ethDBWrapper)(nil)
)

type ethDBWrapper struct {
	db database.Database
}

func New(db database.Database) ethdb.KeyValueStore { return ethDBWrapper{db} }

func (ethDBWrapper) Stat(string) (string, error) { return "", errStatNotSupported }

func (db ethDBWrapper) NewBatch() ethdb.Batch { return ethBatchWrapper{db.db.NewBatch()} }

func (db ethDBWrapper) Has(key []byte) (bool, error) { return db.db.Has(key) }

func (db ethDBWrapper) Get(key []byte) ([]byte, error) { return db.db.Get(key) }

func (db ethDBWrapper) Put(key, value []byte) error { return db.db.Put(key, value) }

func (db ethDBWrapper) Delete(key []byte) error { return db.db.Delete(key) }

func (db ethDBWrapper) Compact(start, limit []byte) error { return db.db.Compact(start, limit) }

func (db ethDBWrapper) Close() error { return db.db.Close() }

func (db ethDBWrapper) NewBatchWithSize(int) ethdb.Batch { return ethBatchWrapper{db.db.NewBatch()} }

func (ethDBWrapper) NewSnapshot() (ethdb.Snapshot, error) {
	return nil, errSnapshotNotSupported
}

func (db ethDBWrapper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	newStart := make([]byte, len(prefix)+len(start))
	copy(newStart, prefix)
	copy(newStart[len(prefix):], start)
	start = newStart

	return db.db.NewIteratorWithStartAndPrefix(start, prefix)
}

func (db ethDBWrapper) NewIteratorWithStart(start []byte) ethdb.Iterator {
	return db.db.NewIteratorWithStart(start)
}

type ethBatchWrapper struct{ database.Batch }

func (e ethBatchWrapper) ValueSize() int { return e.Batch.Size() }

func (e ethBatchWrapper) Replay(w ethdb.KeyValueWriter) error { return e.Batch.Replay(w) }
