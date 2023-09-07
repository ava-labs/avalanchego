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

// Get retrieves the given key if it's present in the key-value data store.
func (reader *dbHeightReader) Get(key []byte) ([]byte, error) {
	iterator := reader.db.rawDB.NewIteratorWithStart(newKey(key, reader.height).Bytes())

	reader.heightLastFoundKey = 0

	if !iterator.Next() {
		// There is no available key with the requested prefix
		return nil, database.ErrNotFound
	}
	resultKey, err := parseRawDBKey(iterator.Key())
	if err != nil {
		return nil, err
	}

	_, isMetadata := resultKey.(*dbMetaKey)

	if isMetadata || !bytes.Equal(resultKey.InnerKey(), key) {
		return nil, database.ErrNotFound
	}

	rawValue := iterator.Value()
	rawValueLen := len(rawValue) - 1
	if rawValue[rawValueLen] == 1 {
		return nil, database.ErrNotFound
	}

	value := rawValue[:rawValueLen]
	reader.heightLastFoundKey = resultKey.Height()

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
