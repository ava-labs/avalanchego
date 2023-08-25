// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package archivedb

import (
	"encoding/binary"
	"errors"
	"math"
)

var (
	ErrInsufficientLength = errors.New("packer has insufficient length for input")
	longLen               = 8
	boolLen               = 1
)

// keyInternal
//
// The keys contains a few extra information alongside the given key. The key is
// what is known outside of this internal scope as the key, but this struct has
// more information compacted inside the keyInternal to take advantage of natural
// sorting. The inversed height is stored right after the Prefix, which is
// MaxUint64 minus the desired Height. An extra byte is also packed inside the
// key to indicate if the key is a deletion of any previous value (because
// archivedb is an append only database).
//
// Any other property are to not serialized but they are useful when parsing a
// keyInternal struct from the database
type keyInternal struct {
	Prefix    []byte
	Height    uint64
	IsDeleted bool
}

// Creates a new Key struct with a given key and its height
func newKey(key []byte, height uint64) *keyInternal {
	return &keyInternal{
		Prefix:    key,
		IsDeleted: false,
		Height:    height,
	}
}

func (k *keyInternal) Bytes() []byte {
	prefixLen := len(k.Prefix)
	bytes := make([]byte, prefixLen+longLen+boolLen)
	copy(bytes[0:], k.Prefix[:])
	binary.BigEndian.PutUint64(bytes[prefixLen:], math.MaxUint64-k.Height)
	if k.IsDeleted {
		bytes[prefixLen+longLen] = 1
	} else {
		bytes[prefixLen+longLen] = 0
	}
	return bytes
}

// Takes a slice of bytes and returns a Key struct
func parseKey(keyBytes []byte) (*keyInternal, error) {
	var key keyInternal
	if longLen+boolLen >= len(keyBytes) {
		return nil, ErrInsufficientLength
	}

	prefixLen := len(keyBytes) - longLen - boolLen

	key.Prefix = make([]byte, prefixLen)
	key.Height = math.MaxUint64 - binary.BigEndian.Uint64(keyBytes[prefixLen:])
	key.IsDeleted = keyBytes[prefixLen+longLen] == 1
	copy(key.Prefix, keyBytes[0:prefixLen])

	return &key, nil
}
