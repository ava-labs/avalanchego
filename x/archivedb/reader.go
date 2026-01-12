// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import "github.com/ava-labs/avalanchego/database"

var _ database.KeyValueReader = (*Reader)(nil)

type Reader struct {
	db     *Database
	height uint64
}

func (r *Reader) Has(key []byte) (bool, error) {
	_, err := r.Get(key)
	if err == database.ErrNotFound {
		return false, nil
	}
	return true, err
}

func (r *Reader) Get(key []byte) ([]byte, error) {
	value, _, exists, err := r.GetEntry(key)
	if err != nil {
		return nil, err
	}
	if exists {
		return value, nil
	}
	return value, database.ErrNotFound
}

// GetEntry retrieves the value of the provided key, the height it was last
// modified at, and a boolean to indicate if the last modification was an
// insertion. If the key has never been modified, ErrNotFound will be returned.
func (r *Reader) GetEntry(key []byte) ([]byte, uint64, bool, error) {
	it := r.db.db.NewIteratorWithStartAndPrefix(newDBKeyFromUser(key, r.height))
	defer it.Release()

	next := it.Next()
	if err := it.Error(); err != nil {
		return nil, 0, false, err
	}

	// There is no available key with the requested prefix
	if !next {
		return nil, 0, false, database.ErrNotFound
	}

	_, height, err := parseDBKeyFromUser(it.Key())
	if err != nil {
		return nil, 0, false, err
	}

	value, exists := parseDBValue(it.Value())
	if !exists {
		return nil, height, false, nil
	}
	return value, height, true, nil
}
