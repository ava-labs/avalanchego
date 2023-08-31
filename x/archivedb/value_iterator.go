// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import "github.com/ava-labs/avalanchego/database"

type allKeysAtHeightIterator struct {
	db          *archiveDB
	keyIterator keysIterator
	height      uint64
	lastKey     []byte
	lastValue   []byte
	lastHeight  uint64
	lastErr     error
}

func (i *allKeysAtHeightIterator) Next() bool {
	for {
		if !i.keyIterator.Next() {
			return false
		}
		key := i.keyIterator.Key()
		value, height, err := i.db.Get(key, i.height)
		if err == database.ErrNotFound {
			// key was not set or deleted at requested height, go to the next
			// key
			continue
		}
		i.lastErr = err
		i.lastKey = key
		i.lastValue = value
		i.lastHeight = height
		return i.lastErr == nil
	}
}

func (i *allKeysAtHeightIterator) Release() {
	i.keyIterator.Release()
}

func (i *allKeysAtHeightIterator) Error() error {
	if i.lastErr != nil {
		return i.lastErr
	}
	return i.keyIterator.Error()
}

func (i *allKeysAtHeightIterator) Height() uint64 {
	return i.lastHeight
}

func (i *allKeysAtHeightIterator) Key() []byte {
	return i.lastKey
}

func (i *allKeysAtHeightIterator) Value() []byte {
	return i.lastValue
}
