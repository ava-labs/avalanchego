// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"encoding/binary"
	"errors"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	ErrEmptyKey           = errors.New("empty key")
	ErrParsingKeyLength   = errors.New("failed reading key length")
	ErrIncorrectKeyLength = errors.New("incorrect key length")

	heightKey = []byte{1}
)

// newDBKey converts a user formatted key and a height into a database formatted
// key.
//
// A database key contains additional information alongside the given user key.
//
// The requirements of a database key are:
//
// 1. A given user key must have a unique database key prefix. This guarantees
// that user keys can not overlap on disk.
// 2. Inside of a database key prefix, the database keys must be sorted by
// decreasing height.
// 3. User keys must never overlap with any metadata keys.
//
// To meet these requirements, a database key prefix is defined by concatinating
// the zero byte, the length of the user key, and the user key. The suffix of
// the database key is the negation of the big endian encoded height. This
// suffix guarantees the keys are sorted correctly.
//
//	Example (Asumming heights are 1 byte):
//	 |  User given  |  Stored as  |
//	 |--------------|-------------|
//	 |    foo:10    |  3:foo:245  |
//	 |    foo:20    |  3:foo:235  |
//
// Returns:
// - The database key
// - The database key prefix, which is independent of the height
func newDBKey(key []byte, height uint64) ([]byte, []byte) {
	keyLen := len(key)
	dbKeyMaxSize := 1 + binary.MaxVarintLen64 + keyLen + wrappers.LongLen
	dbKey := make([]byte, dbKeyMaxSize)
	offset := 1
	offset += binary.PutUvarint(dbKey[offset:], uint64(keyLen))
	offset += copy(dbKey[offset:], key)
	prefixOffset := offset
	binary.BigEndian.PutUint64(dbKey[offset:], ^height)
	offset += wrappers.LongLen
	return dbKey[:offset], dbKey[:prefixOffset]
}

// parseDBKey takes a database formatted key and returns the user formatted key
// along with its height.
//
// Note: An error should only be returned from this function if the database has
// been corrupted.
func parseDBKey(dbKey []byte) ([]byte, uint64, error) {
	dbKeyLen := uint64(len(dbKey))
	if dbKeyLen == 0 {
		return nil, 0, ErrEmptyKey
	}

	keyLen, offset := binary.Uvarint(dbKey[1:])
	if offset <= 0 {
		return nil, 0, ErrParsingKeyLength
	}

	keyIndex := 1 + uint64(offset)
	heightIndex := keyIndex + keyLen
	if dbKeyLen != heightIndex+wrappers.LongLen {
		return nil, 0, ErrIncorrectKeyLength
	}

	key := dbKey[keyIndex:heightIndex]
	height := ^binary.BigEndian.Uint64(dbKey[heightIndex:])
	return key, height, nil
}
