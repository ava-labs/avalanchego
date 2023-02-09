// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database"
)

var _ database.Batch = &batch{}

type batchOp struct {
	key    []byte
	value  []byte
	delete bool
}

// batch is a write-only database that commits changes to its host database
// when Write is called.
type batch struct {
	db   *Database
	data []batchOp
	size int
}

// on [Write], put key, value into the database
func (b *batch) Put(key []byte, value []byte) error {
	b.data = append(b.data, batchOp{
		key:   slices.Clone(key),
		value: slices.Clone(value),
	})
	b.size += len(key) + len(value)
	return nil
}

// on [Write], delete key from database
func (b *batch) Delete(key []byte) error {
	b.data = append(b.data, batchOp{
		key:    slices.Clone(key),
		delete: true,
	})
	b.size += len(key)
	return nil
}

func (b *batch) Size() int {
	return b.size
}

// apply all operations in order to the database and write the result to disk
func (b *batch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	return b.db.commitBatch(b.data)
}

func (b *batch) Reset() {
	b.data = nil
	b.size = 0
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, op := range b.data {
		if op.delete {
			if err := w.Delete(op.key); err != nil {
				return err
			}
		} else if err := w.Put(op.key, op.value); err != nil {
			return err
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch {
	return b
}
