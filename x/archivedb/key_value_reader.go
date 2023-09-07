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

// Has() retrieves if a key is present in the key-value data store.
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
	iterator := reader.db.rawDB.NewIteratorWithStart(newDBKey(key, reader.height))

	reader.heightLastFoundKey = 0

	if !iterator.Next() {
		// There is no available key with the requested prefix
		return nil, database.ErrNotFound
	}
	foundKey, height, err := parseDBKey(iterator.Key())
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(foundKey, key) {
		return nil, database.ErrNotFound
	}
	rawValue := iterator.Value()
	rawValueLen := len(rawValue) - 1
	if rawValue[rawValueLen] == 1 {
		return nil, database.ErrNotFound
	}
	value := rawValue[:rawValueLen]
	reader.heightLastFoundKey = height
	return value, nil
}

// Returns the height value where a key has been found. If the last key was not
// found an error will be thrown
func (reader *dbHeightReader) GetHeightFromLastFoundKey() (uint64, error) {
	if reader.heightLastFoundKey == 0 {
		return 0, database.ErrNotFound
	}
	return reader.heightLastFoundKey, nil
}
