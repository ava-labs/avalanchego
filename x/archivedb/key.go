// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"encoding/binary"
	"errors"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	ErrParsingKeyLength   = errors.New("failed reading key length")
	ErrIncorrectKeyLength = errors.New("incorrect key length")

	heightKey = newDBKeyFromMetadata([]byte{})
)

// The requirements of a database key are:
//
// 1. A given user key must have a unique database key prefix. This guarantees
// that user keys can not overlap on disk.
// 2. Inside of a database key prefix, the database keys must be sorted by
// decreasing height.
// 3. User keys must never overlap with any metadata keys.

// newDBKeyFromUser converts a user key and height into a database formatted
// key.
//
// To meet the requirements of a database key, the prefix is defined by
// concatenating the length of the user key and the user key. The suffix of the
// database key is the negation of the big endian encoded height. This suffix
// guarantees the keys are sorted correctly.
//
//	Example (Asumming heights are 1 byte):
//	 |  User key  |  Stored as  |
//	 |------------|-------------|
//	 |   foo:10   |  3:foo:245  |
//	 |   foo:20   |  3:foo:235  |
//
// Returns:
// - The database key
// - The database key prefix, which is independent of the height
func newDBKeyFromUser(key []byte, height uint64) ([]byte, []byte) {
	keyLen := len(key)
	dbKeyMaxSize := binary.MaxVarintLen64 + keyLen + wrappers.LongLen
	dbKey := make([]byte, dbKeyMaxSize)
	offset := binary.PutUvarint(dbKey, uint64(keyLen))
	offset += copy(dbKey[offset:], key)
	prefixOffset := offset
	binary.BigEndian.PutUint64(dbKey[offset:], ^height)
	offset += wrappers.LongLen
	return dbKey[:offset], dbKey[:prefixOffset]
}

// parseDBKeyFromUser takes a database formatted key and returns the user key
// along with its height.
//
// Note: An error should only be returned from this function if the database has
// been corrupted.
func parseDBKeyFromUser(dbKey []byte) ([]byte, uint64, error) {
	keyLen, offset := binary.Uvarint(dbKey)
	if offset <= 0 {
		return nil, 0, ErrParsingKeyLength
	}

	heightIndex := uint64(offset) + keyLen
	if uint64(len(dbKey)) != heightIndex+wrappers.LongLen {
		return nil, 0, ErrIncorrectKeyLength
	}

	key := dbKey[offset:heightIndex]
	height := ^binary.BigEndian.Uint64(dbKey[heightIndex:])
	return key, height, nil
}

// newDBKeyFromMetadata converts a metadata key into a database formatted key.
//
// To meet the requirements of a database key, the key is defined by
// concatenating the length of the metadata key + 1 and the metadata key.
//
//	Example:
//	 |  Metadata key  |  Stored as  |
//	 |----------------|-------------|
//	 |       foo      |    4:foo    |
//	 |       fo       |    3:fo     |
func newDBKeyFromMetadata(key []byte) []byte {
	keyLen := len(key)
	dbKeyMaxSize := binary.MaxVarintLen64 + keyLen
	dbKey := make([]byte, dbKeyMaxSize)
	offset := binary.PutUvarint(dbKey, uint64(keyLen)+1)
	offset += copy(dbKey[offset:], key)
	return dbKey[:offset]
}
