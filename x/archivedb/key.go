// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var ErrInsufficientLength = errors.New("packer has insufficient length for input")

const (
	internalKeySuffixLen = wrappers.LongLen
	minInternalKeyLen    = internalKeySuffixLen + 1
)

// dbKey
//
// The keys contains a few extra information alongside the given key. The key is
// what is known outside of this internal scope as the key, but this struct has
// more information compacted inside the dbKey to take advantage of natural
// sorting. The inversed height is stored right after the Prefix, which is
// MaxUint64 minus the desired Height.
//
// The inverse height is stored, instead of the height, as part of the key. That
// is because the database interface only provides ascending iterators.
// Archivedb's main usage is to query a given key at a given height, or the
// immediate previous height. That is why the heights are being converted from
// `height` to `MAX_INT64 - height`.
//
//	Example (Asumming MAX_INT64 is 10,000):
//	 |  User given  |  Stored as   |
//	 |--------------|--------------|
//	 |    foo:10    |   foo:9910   |
//	 |    foo:20    |   foo:9080   |
//
// The internal sorting will be `foo:9080`, `foo:9910` instead of (`foo:10`,
// `foo:20`). The new sorting plays well with the descending iterator, having a
// O(1) operation to fetch the exact match or the previous value at a given
// height.
//
// All keys are prefixed with a VarUint64, which is the length of the keys, the
// idea is to make reads even simpler, making it effectively a O(1) operation.
func newDBKey(key []byte, height uint64) []byte {
	keyLen := len(key)
	rawKeyMaxSize := keyLen + binary.MaxVarintLen64 + wrappers.LongLen
	rawKey := make([]byte, rawKeyMaxSize)
	offset := binary.PutUvarint(rawKey, uint64(keyLen))
	offset += copy(rawKey[offset:], key)
	binary.BigEndian.PutUint64(rawKey[offset:], ^height)
	offset += wrappers.LongLen

	return rawKey[0:offset]
}

// parseDBKey takes a slice of bytes and returns the inner key and the height
func parseDBKey(rawKey []byte) ([]byte, uint64, error) {
	rawKeyLen := len(rawKey)
	if rawKeyLen < minInternalKeyLen {
		return nil, 0, ErrInsufficientLength
	}

	reader := bytes.NewReader(rawKey)
	// ReadUvarint cannot fail since the minInternalKeyLen makes sure there are
	// enough bytes to read a varint
	keyLen, _ := binary.ReadUvarint(reader)

	key := make([]byte, keyLen)
	readBytes, err := reader.Read(key)
	if err != nil {
		return nil, 0, err
	}

	if uint64(readBytes) != keyLen {
		return nil, 0, database.ErrNotFound
	}

	// Read th inversed height, it will be converted to height using `^` just
	// before returning. Read above why the inversed height is used instead of a
	// normal height
	inversedHeight := binary.BigEndian.Uint64(rawKey[rawKeyLen-wrappers.LongLen:])

	return key, ^inversedHeight, nil
}
