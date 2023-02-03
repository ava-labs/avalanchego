// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
)

var _ database.Batch = &batch{}

type batchOp struct {
	value  []byte
	delete bool
}

// batch is a write-only database that commits changes to its host database
// when Write is called.
type batch struct {
	db   *Database
	data linkedhashmap.LinkedHashmap[string, batchOp]
	size int
}

// on [Write], put key, value into the database
func (b *batch) Put(key []byte, value []byte) error {
	b.putOp(
		key,
		batchOp{
			value: slices.Clone(value),
		},
	)
	return nil
}

// on [Write], delete key from database
func (b *batch) Delete(key []byte) error {
	b.putOp(
		key,
		batchOp{
			delete: true,
		},
	)
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
	b.data = linkedhashmap.New[string, batchOp]()
	b.size = 0
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	it := b.data.NewIterator()
	for it.Next() {
		key := []byte(it.Key())
		op := it.Value()
		if op.delete {
			if err := w.Delete(key); err != nil {
				return err
			}
		} else if err := w.Put(key, op.value); err != nil {
			return err
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch {
	return b
}

func (b *batch) putOp(key []byte, op batchOp) {
	stringKey := string(key)
	lenKey := len(key)
	if existing, ok := b.data.Get(stringKey); ok {
		b.size -= lenKey + len(existing.value)
	}
	b.data.Put(stringKey, op)
	b.size += lenKey + len(op.value)
}
