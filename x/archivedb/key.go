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
	// This default prefix is used as the internal default prefix for all keys.
	// By doing so the database can safely accept keys from untrusted sources,
	// by using a common prefix no matter how the keys are crafted externally
	// they won't interfere with the database
	//
	// Having this common prefix also also allows archivedb to store metadata
	// that may be needed, without polluting the key space.
	//
	// Another side effect is that migrations are possible, by changing the
	// prefix namespace, pulling the old prefix and migrating to the new one.
	dataPrefix    = []byte("v1")
	dataPrefixLen = len(dataPrefix)
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
	key       []byte
	height    uint64
	isDeleted bool
}

// Creates a new Key struct with a given key and its height
func newInternalKey(key []byte, height uint64) *keyInternal {
	return &keyInternal{
		key:       key,
		isDeleted: false,
		height:    height,
	}
}

func (k *keyInternal) Bytes() []byte {
	prefixLen := len(k.key)
	bytes := make([]byte, prefixLen+longLen+boolLen+dataPrefixLen)
	copy(bytes[0:], dataPrefix)
	copy(bytes[dataPrefixLen:], k.key)
	binary.BigEndian.PutUint64(bytes[dataPrefixLen+prefixLen:], math.MaxUint64-k.height)
	if k.isDeleted {
		bytes[dataPrefixLen+prefixLen+longLen] = 1
	} else {
		bytes[dataPrefixLen+prefixLen+longLen] = 0
	}
	return bytes
}

// Takes a slice of bytes and returns a Key struct
func parseKey(keyBytes []byte) (*keyInternal, error) {
	var key keyInternal
	if longLen+boolLen+dataPrefixLen >= len(keyBytes) {
		return nil, ErrInsufficientLength
	}

	prefixLen := len(keyBytes) - longLen - boolLen

	key.key = make([]byte, prefixLen-dataPrefixLen)
	key.height = math.MaxUint64 - binary.BigEndian.Uint64(keyBytes[prefixLen:])
	key.isDeleted = keyBytes[prefixLen+longLen] == 1
	copy(key.key, keyBytes[dataPrefixLen:dataPrefixLen+prefixLen])

	return &key, nil
}
