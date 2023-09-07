// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
)

var (
	ErrInsufficientLength = errors.New("packer has insufficient length for input")
	longLen               = 8
	internalKeySuffixLen  = longLen
	minInternalKeyLen     = internalKeySuffixLen + 2
)

// Database Key
//
// A key must implement this interface in order to be stored in the database.
//
// It should expose the inner key, the raw bytes representation as it will be
// stored in the database, and a height at which this key should be associated.
//
// There are two specific implementations, a dbKey and a dbMetaKey. The dbKeye
// is used to store any record in the database and dbMetaKey to store any
// metadata that the database needs to function (and it is a global entry, not
// associated to any particual height).
//
// By definition the values associated with a dbKey are append only, there is no
// support to delete or update it. A new new record with the inner key can be
// stored, but that will have a different dbKey because the heights will be
// different. The entries defined under dbMetaKey can be updated onsight.
//
// There is no external API to use the dbMetaKey, for now it is reserved for
// internal usage only.
type databaseKey interface {
	InnerKey() []byte
	Bytes() []byte
	Height() uint64
}

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
type dbKey struct {
	key    []byte
	height uint64
}

// Creates a new Key struct with a given key and its height
func newKey(key []byte, height uint64) *dbKey {
	return &dbKey{
		key:    key,
		height: height,
	}
}

func (k *dbKey) Bytes() []byte {
	keyLen := len(k.key)
	keyLenBytes := binary.AppendUvarint([]byte{}, uint64(keyLen))

	bytes := make([]byte, len(keyLenBytes)+keyLen+internalKeySuffixLen)
	offset := copy(bytes[0:], keyLenBytes)
	offset += copy(bytes[offset:], k.key)
	binary.BigEndian.PutUint64(bytes[offset:], math.MaxUint64-k.height)
	return bytes
}

func (k *dbKey) Height() uint64 {
	return k.height
}

func (k *dbKey) InnerKey() []byte {
	return k.key
}

type dbMetaKey struct {
	key []byte
}

func newMetaKey(key []byte) *dbMetaKey {
	return &dbMetaKey{key}
}

func (k *dbMetaKey) Bytes() []byte {
	bytes := make([]byte, len(k.key)+binary.MaxVarintLen64)
	binary.PutUvarint(bytes, math.MaxUint64)
	copy(bytes[binary.MaxVarintLen64:], k.key)
	return bytes
}

func (dbMetaKey) Height() uint64 {
	return 0
}

func (k *dbMetaKey) InnerKey() []byte {
	return k.key
}

// Takes a slice of bytes and returns a databaseKey instance.
func parseRawDBKey(rawKey []byte) (databaseKey, error) {
	rawKeyLen := len(rawKey)
	reader := bytes.NewReader(rawKey)
	keyLen, err := binary.ReadUvarint(reader)
	if err != nil {
		return nil, err
	}

	if keyLen == math.MaxUint64 {
		return &dbMetaKey{
			key: rawKey[binary.MaxVarintLen64:],
		}, nil
	}

	if minInternalKeyLen >= rawKeyLen {
		return nil, ErrInsufficientLength
	}

	key := make([]byte, keyLen)
	readBytes, err := reader.Read(key)
	if err != nil {
		return nil, err
	}

	if uint64(readBytes) != keyLen {
		return nil, ErrInsufficientLength
	}

	return &dbKey{
		height: math.MaxUint64 - binary.BigEndian.Uint64(rawKey[rawKeyLen-internalKeySuffixLen:]),
		key:    key,
	}, nil
}
