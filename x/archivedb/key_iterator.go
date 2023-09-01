// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import "github.com/ava-labs/avalanchego/database"

type keysIterator struct {
	db        *archiveDB
	prefix    []byte
	lastKey   []byte
	lastValue uint64
	lastErr   error
}

// The core algorithm of this key iterator
//
// This value will attempt to load all the known keys that matches a given
// prefix. The algorithm is quite simple and it leverages the natural sorting of
// keys.
//
// The first key that matches a prefix is loaded, as the *first* key. Subsequent
// keys are loaded as the previous key from the last known key at height 0
// (remember that height 0 does not exists, they do exists from 1 to N), so by
// using height 0 we make sure that the previous key is a different key that is
// close to the current key, potentially sharing the same prefix. If the
// prefixes are different or not key cannot be found, the iterator stops
//
// Imagine we have a database with the following records:
//
//   - prefix:a:100 		-> value for a”?
//   - prefix:a:10 		-> value for a
//   - prefix:c:1000 	-> value for c'
//   - prefix:c:9 		-> value for c
//   - prefix:z:99 		-> value for z
//
// When querying for all the keys under `prefix:` the first record to match is
// going to be `prefix:a:100`, which matches the requested prefix. On the second
// iteration this function is going to query the next key after `prefix:a:0`,
// which is going to be `prefix:c:1000`. Same thing happens on the next
// iteration, querying for the next key after `prefix:c:0`, which is
// `prefix:z:99`.
func (i *keysIterator) Next() bool {
	var iterator database.Iterator
	if i.lastKey == nil {
		iterator = i.db.rawDB.NewIteratorWithPrefix(i.prefix)
	} else {
		iterator = i.db.rawDB.NewIteratorWithStartAndPrefix(newInternalKey(i.lastKey, 0).Bytes(), i.prefix)
	}
	if !iterator.Next() {
		return false
	}
	lastKey, err := parseKey(iterator.Key())
	if err != nil {
		return false
	}
	i.lastErr = iterator.Error()
	i.lastKey = lastKey.key
	i.lastValue = lastKey.height
	return i.lastErr == nil
}

func (i *keysIterator) Error() error {
	return i.lastErr
}

func (i *keysIterator) Key() []byte {
	return i.lastKey
}

func (i *keysIterator) Value() uint64 {
	return i.lastValue
}

func (*keysIterator) Release() {
}
