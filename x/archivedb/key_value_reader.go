// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"bytes"

	"github.com/ava-labs/avalanchego/database"
)

type dbHeightReader struct {
	db                 *archiveDB
	height             uint64
	heightLastFoundKey uint64
}

// Builds a database.Iterator where the next value is the requested *value*. If
// the value is not found a database.ErrNotFound is returned.
//
// This is a private function which helps Get() and Has() to be share the key
// look up logic
func (reader *dbHeightReader) getIteratorToValue(key []byte) (database.Iterator, error) {
	internalKey := newInternalKey(key, reader.height)
	iterator := reader.db.rawDB.NewIteratorWithStart(internalKey.Bytes())
	keyLength := len(key)

	reader.heightLastFoundKey = 0

	for {
		if !iterator.Next() {
			// There is no available key with the requested prefix
			return iterator, database.ErrNotFound
		}
		internalKey, err := parseKey(iterator.Key())
		if err != nil {
			return iterator, err
		}
		if !bytes.Equal(internalKey.key, key) {
			if keyLength < len(internalKey.key) {
				// The current key is a longer than the requested key, now check
				// if they match at the same length as `key`, if that is the
				// case we should continue to the next key, until the exact
				// requested key is found or anothe prefix is found and by that
				// point it would exit
				if bytes.Equal(internalKey.key[0:keyLength], key) {
					// Same prefix, read the next key until the prefix is
					// different or the exact requested key is found
					continue
				}
			}
			// The previous key that was found does has another prefix, because the
			// iterator is not aware of prefixes. If this happens it means the
			// prefix at the requested height does not exists.
			return iterator, database.ErrNotFound
		}

		if internalKey.isDeleted {
			// The database is append only, so when removing a record creates a new
			// record with an special flag is being created. Before returning the
			// value we check if the deleted flag is present or not.
			return iterator, database.ErrNotFound
		}

		reader.heightLastFoundKey = internalKey.height

		return iterator, nil
	}
}

// Has retrieves if a key is present in the key-value data store.
func (reader *dbHeightReader) Has(key []byte) (bool, error) {
	iterator, err := reader.getIteratorToValue(key)
	defer iterator.Release()
	if err == database.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Get retrieves the given key if it's present in the key-value data store.
func (reader *dbHeightReader) Get(key []byte) ([]byte, error) {
	iterator, err := reader.getIteratorToValue(key)
	defer iterator.Release()
	if err != nil {
		return nil, err
	}
	return iterator.Value(), nil
}

// Returns the height value where a key has been found. If the last key was not
// found an error will be thrown
func (reader *dbHeightReader) GetHeightFromLastFoundKey() (uint64, error) {
	if reader.heightLastFoundKey == 0 {
		return 0, database.ErrNotFound
	}
	return reader.heightLastFoundKey, nil
}
