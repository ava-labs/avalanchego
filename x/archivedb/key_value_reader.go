// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"bytes"

	"github.com/ava-labs/avalanchego/database"
)

var _ database.KeyValueReader = (*dbHeightReader)(nil)

type dbHeightReader struct {
	db     *archiveDB
	height uint64
}

// Has retrieves if a key is present in the key-value data store.
func (reader *dbHeightReader) Has(key []byte) (bool, error) {
	_, err := reader.Get(key)
	if err == database.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (reader *dbHeightReader) getValueAndHeight(key []byte) ([]byte, uint64, error) {
	iterator := reader.db.rawDB.NewIteratorWithStart(newDBKey(key, reader.height))
	defer iterator.Release()

	if !iterator.Next() {
		if err := iterator.Error(); err != nil {
			return nil, 0, err
		}

		// There is no available key with the requested prefix
		return nil, 0, database.ErrNotFound
	}

	foundKey, height, err := parseDBKey(iterator.Key())
	if err != nil {
		return nil, 0, err
	}
	if !bytes.Equal(foundKey, key) {
		// A key was found, through the iterator, *but* the prefix is not the
		// same, another key exists, but not the requested one
		//
		// This happens because we search for a key at a given height, or the
		// previous key. In this case the previous key is an unrelated key
		return nil, 0, database.ErrNotFound
	}
	rawValue := iterator.Value()
	rawValueLen := len(rawValue)
	if rawValueLen == 0 {
		// malformed, value should never ever be empty
		return nil, 0, ErrInvalidValue
	}
	if rawValue[rawValueLen-1] == 1 {
		return nil, 0, database.ErrNotFound
	}
	value := rawValue[:rawValueLen-1]
	return value, height, nil
}

// Get retrieves the given key if it's present in the key-value data store.
//
// This is a public API, so the dbKey is used to retrieve any value. It is
// guaranteed to be a O(1), because how the dbKey is structured internally which
// prevents a longer key to match a shorter key with the same prefix.
//
// If the result matches a dbMetaKey it will be consired as a ErrNotFound
// because dbMetaKey is for internal usage and should be leaked outside of the
// module
func (reader *dbHeightReader) Get(key []byte) ([]byte, error) {
	value, _, err := reader.getValueAndHeight(key)
	return value, err
}

// GetHeightFromLastFoundKey returns the height value where a key has been
// found. If the last key was not found an error will be thrown
func (reader *dbHeightReader) GetHeightFromLastFoundKey(key []byte) (uint64, error) {
	_, height, err := reader.getValueAndHeight(key)
	return height, err
}
