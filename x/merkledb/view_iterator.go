// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"

	"github.com/ava-labs/avalanchego/database"
	"golang.org/x/exp/slices"
)

func (t *trieView) NewIterator() database.Iterator {
	return t.NewIteratorWithStartAndPrefix(nil, nil)
}

func (t *trieView) NewIteratorWithStart(start []byte) database.Iterator {
	return t.NewIteratorWithStartAndPrefix(start, nil)
}

func (t *trieView) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return t.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (t *trieView) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	changes := make([]KeyChange, 0, len(t.changes.values))
	for path, change := range t.changes.values {
		key := path.Serialize().Value
		if (len(start) > 0 && bytes.Compare(start, key) > 0) || !bytes.HasPrefix(key, prefix) {
			continue
		}
		changes = append(changes, KeyChange{Key: key, Value: change.after})
	}
	// sort [changes] so they can be merged with the parent trie's state
	slices.SortFunc(changes, func(a, b KeyChange) bool {
		return bytes.Compare(a.Key, b.Key) == -1
	})

	return &viewIterator{
		view:          t,
		Iterator:      t.parentTrie.NewIteratorWithStartAndPrefix(start, prefix),
		sortedChanges: changes,
	}
}

// viewIterator walks over both the in memory database and the underlying database
// at the same time.
type viewIterator struct {
	view *trieView
	database.Iterator

	key, value []byte
	err        error

	sortedChanges []KeyChange

	initialized, exhausted bool
}

// Next moves the iterator to the next key/value pair. It returns whether the
// iterator is exhausted. We must pay careful attention to set the proper values
// based on if the in memory db or the underlying db should be read next
func (it *viewIterator) Next() bool {
	// Short-circuit and set an error if the underlying database has been closed.
	if it.view.db.closed {
		it.key = nil
		it.value = nil
		it.err = database.ErrClosed
		return false
	}

	if it.view.invalidated {
		it.key = nil
		it.value = nil
		it.err = ErrInvalid
		return false
	}

	if !it.initialized {
		it.exhausted = !it.Iterator.Next()
		it.initialized = true
	}

	for {
		switch {
		case it.exhausted && len(it.sortedChanges) == 0:
			// there are no more changes or underlying key/values
			it.key = nil
			it.value = nil
			return false
		case it.exhausted:
			// there are no more underlying key/values, so use the local changes
			nextKey := it.sortedChanges[0].Key
			nextValue := it.sortedChanges[0].Value

			// move to next change
			it.sortedChanges = it.sortedChanges[1:]

			// if current change is not a deletion, return it
			if !nextValue.IsNothing() {
				it.key = nextKey
				it.value = nextValue.value
				return true
			}
		case len(it.sortedChanges) == 0:
			it.key = it.Iterator.Key()
			it.value = it.Iterator.Value()
			it.exhausted = !it.Iterator.Next()
			return true
		default:
			memKey := it.sortedChanges[0].Key
			memValue := it.sortedChanges[0].Value

			dbKey := it.Iterator.Key()

			switch {
			case bytes.Compare(memKey, dbKey) < 0:
				// the current change has a smaller key than the underlying db key
				// move to the next change
				it.sortedChanges = it.sortedChanges[1:]

				// if current change is not a deletion, return it
				if !memValue.IsNothing() {
					it.key = memKey
					it.value = memValue.value
					return true
				}
			case bytes.Compare(dbKey, memKey) < 0:
				// the db key is smaller, so return it and iterate the underlying iterator
				it.key = dbKey
				it.value = it.Iterator.Value()
				it.exhausted = !it.Iterator.Next()
				return true
			default:
				// the keys are the same, so use the local change and
				// iterate both the sorted changes and the underlying iterator
				it.sortedChanges = it.sortedChanges[1:]
				it.exhausted = !it.Iterator.Next()

				if !memValue.IsNothing() {
					it.key = memKey
					it.value = memValue.value
					return true
				}
			}
		}
	}
}

func (it *viewIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.Iterator.Error()
}

func (it *viewIterator) Key() []byte {
	return it.key
}

func (it *viewIterator) Value() []byte {
	return it.value
}

func (it *viewIterator) Release() {
	it.key = nil
	it.value = nil
	it.sortedChanges = nil
	it.Iterator.Release()
}
