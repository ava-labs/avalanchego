package merkledb

import (
	"github.com/ava-labs/avalanchego/database"
)

type batchValue struct {
	key    []byte
	value  []byte
	delete bool
}

// batch is a write-only database that commits changes to its host database
// when Write is called. A batch cannot be used concurrently.
type batch struct {
	data []batchValue
	tree *Tree
}

func NewBatch(t *Tree) database.Batch {
	return &batch{
		data: nil,
		tree: t,
	}
}

// ValueSize retrieves the amount of data queued up for writing.
func (b *batch) ValueSize() int {
	return len(b.data)
}

// Write flushes any accumulated data to disk.
// TODO Optimize
// If the size of the batch is smaller than the trie, which it will be in all realistic cases,
// then it makes sense to do some pre-processing such that the batched operations don't modify the same part of the trie multiple times.
func (b *batch) Write() error {
	var err error
	mutex.Lock()
	defer mutex.Unlock()

	for _, d := range b.data {
		if d.delete {
			err = b.tree.delete(d.key)
		} else {
			err = b.tree.put(d.key, d.value)
		}
	}
	return b.tree.persistence.Commit(err)
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	b.data = nil
}

// Replay replays the batch contents.
func (b *batch) Replay(w database.KeyValueWriter) error {
	for _, val := range b.data {
		if val.delete {
			if err := w.Delete(val.key); err != nil {
				return err
			}
		} else if err := w.Put(val.key, val.value); err != nil {
			return err
		}
	}
	return nil
}

// Put inserts the given value into the key-value data store.
func (b *batch) Put(key []byte, value []byte) error {
	b.data = append(b.data, batchValue{
		key:    key,
		value:  value,
		delete: false,
	})
	return nil
}

// Delete removes the key from the key-value data store.
func (b *batch) Delete(key []byte) error {
	b.data = append(b.data, batchValue{
		key:    key,
		value:  nil,
		delete: true,
	})
	return nil
}

// Inner returns a batch writing to the inner database, if one exists. If
// this batch is already writing to the base DB, then itself should be
// returned.
func (b *batch) Inner() database.Batch {
	return b
}
