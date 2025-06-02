// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"slices"

	"github.com/ava-labs/avalanchego/database"
)

func (v *view) NewIterator() database.Iterator {
	return v.NewIteratorWithStartAndPrefix(nil, nil)
}

func (v *view) NewIteratorWithStart(start []byte) database.Iterator {
	return v.NewIteratorWithStartAndPrefix(start, nil)
}

func (v *view) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return v.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (v *view) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	var (
		changes   = make([]KeyChange, 0, len(v.changes.keyChanges))
		startKey  = ToKey(start)
		prefixKey = ToKey(prefix)
	)

	startKeyIndex := 0
	if len(start) > 0 {
		// Binary search for [startKey] index.
		startKeyIndex, _ = slices.BinarySearchFunc(v.changes.sortedKeys, startKey, func(key1 Key, key2 Key) int {
			return key1.Compare(key2)
		})
	}

	for _, key := range v.changes.sortedKeys[startKeyIndex:] {
		if !key.HasPrefix(prefixKey) {
			if len(changes) > 0 {
				// Since [sortedKeyChanges] is sorted, if the prefix isnt found anymore after we
				// added at least one [KeyChange], we can stop.
				break
			}

			continue
		}

		changes = append(changes, KeyChange{
			Key:   key.Bytes(),
			Value: v.changes.keyChanges[key].after,
		})
	}

	return &viewIterator{
		view:          v,
		parentIter:    v.parentTrie.NewIteratorWithStartAndPrefix(start, prefix),
		sortedChanges: changes,
	}
}

// viewIterator walks over both the in memory database and the underlying database
// at the same time.
type viewIterator struct {
	view       *view
	parentIter database.Iterator

	key, value []byte
	err        error

	sortedChanges []KeyChange

	initialized, parentIterExhausted bool
}

// Next moves the iterator to the next key/value pair. It returns whether the
// iterator is exhausted. We must pay careful attention to set the proper values
// based on if the in memory changes or the underlying db should be read next
func (it *viewIterator) Next() bool {
	switch {
	case it.view.isInvalid():
		it.key = nil
		it.value = nil
		it.err = ErrInvalid
		return false
	case !it.initialized:
		it.parentIterExhausted = !it.parentIter.Next()
		it.initialized = true
	}

	for {
		switch {
		case it.parentIterExhausted && len(it.sortedChanges) == 0:
			// there are no more changes or underlying key/values
			it.key = nil
			it.value = nil
			return false
		case it.parentIterExhausted:
			// there are no more underlying key/values, so use the local changes
			nextKeyValue := it.sortedChanges[0]

			// move to next change
			it.sortedChanges = it.sortedChanges[1:]

			// If current change is not a deletion, return it.
			// Otherwise go to next loop iteration.
			if !nextKeyValue.Value.IsNothing() {
				it.key = nextKeyValue.Key
				it.value = nextKeyValue.Value.Value()
				return true
			}
		case len(it.sortedChanges) == 0:
			it.key = it.parentIter.Key()
			it.value = it.parentIter.Value()
			it.parentIterExhausted = !it.parentIter.Next()
			return true
		default:
			memKey := it.sortedChanges[0].Key
			memValue := it.sortedChanges[0].Value

			parentKey := it.parentIter.Key()

			switch bytes.Compare(memKey, parentKey) {
			case -1:
				// The current change has a smaller key than the parent key.
				// Move to the next change.
				it.sortedChanges = it.sortedChanges[1:]

				// If current change is not a deletion, return it.
				// Otherwise, go to next loop iteration.
				if memValue.HasValue() {
					it.key = memKey
					it.value = slices.Clone(memValue.Value())
					return true
				}
			case 1:
				// The parent key is smaller, so return it and iterate the parent iterator
				it.key = parentKey
				it.value = it.parentIter.Value()
				it.parentIterExhausted = !it.parentIter.Next()
				return true
			default:
				// the keys are the same, so use the local change and
				// iterate both the sorted changes and the parent iterator
				it.sortedChanges = it.sortedChanges[1:]
				it.parentIterExhausted = !it.parentIter.Next()

				if memValue.HasValue() {
					it.key = memKey
					it.value = slices.Clone(memValue.Value())
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
	return it.parentIter.Error()
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
	it.parentIter.Release()
}
