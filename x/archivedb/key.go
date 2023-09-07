// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
)

var (
	ErrInsufficientLength = errors.New("packer has insufficient length for input")
	longLen               = 8
	internalKeySuffixLen  = longLen
	minInternalKeyLen     = internalKeySuffixLen + 2
)

// dbKey
//
// The keys contains a few extra information alongside the given key. The key is
// what is known outside of this internal scope as the key, but this struct has
// more information compacted inside the dbKey to take advantage of natural
// sorting. The inversed height is stored right after the Prefix, which is
// MaxUint64 minus the desired Height.
//
// All keys are prefixed with a VarUint64, which is the length of the keys, the
// idea is to make reads even simpler, making it effectively a O(1) operation.
// Before this change it would have been O(1) unless a another key existed with
// the same prefix as the requested. By appending the length of the inner key as
// the first bytes, it is not longer possible to query by a prefix (the full key
// is needed to do a lookup) but all reads are a O(1).
func newDBKey(key []byte, height uint64) []byte {
	keyLen := len(key)
	rawKeyMaxSize := keyLen + binary.MaxVarintLen64 + longLen
	rawKey := make([]byte, rawKeyMaxSize)
	offset := binary.PutUvarint(rawKey, uint64(keyLen))
	offset += copy(rawKey[offset:], key)
	binary.BigEndian.PutUint64(rawKey[offset:], ^height)
	offset += longLen

	return rawKey[0:offset]
}

// Takes a slice of bytes and returns a databaseKey instance.
func parseDBKey(rawKey []byte) ([]byte, uint64, error) {
	if bytes.Equal(rawKey, keyHeight) {
		return nil, 0, database.ErrNotFound
	}
	rawKeyLen := len(rawKey)
	reader := bytes.NewReader(rawKey)
	keyLen, err := binary.ReadUvarint(reader)
	if err != nil {
		return nil, 0, err
	}

	if minInternalKeyLen >= rawKeyLen {
		return nil, 0, ErrInsufficientLength
	}

	key := make([]byte, keyLen)
	readBytes, err := reader.Read(key)
	if err != nil {
		return nil, 0, err
	}

	if uint64(readBytes) != keyLen {
		panic(fmt.Sprintf("foo %d %d", keyLen, readBytes))
		return nil, 0, ErrInsufficientLength
	}

	inversedHeight := binary.BigEndian.Uint64(rawKey[rawKeyLen-longLen:])

	return key, ^inversedHeight, nil
}
