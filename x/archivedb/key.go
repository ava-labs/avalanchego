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
	key    []byte
	height uint64
}

type dbKey interface {
	InnerKey() []byte
	Bytes() []byte
	Height() uint64
}

// Creates a new Key struct with a given key and its height
func newKey(key []byte, height uint64) *keyInternal {
	return &keyInternal{
		key:    key,
		height: height,
	}
}

func (k *keyInternal) Bytes() []byte {
	keyLen := len(k.key)
	keyLenBytes := binary.AppendUvarint([]byte{}, uint64(keyLen))

	bytes := make([]byte, len(keyLenBytes)+keyLen+internalKeySuffixLen)
	offset := copy(bytes[0:], keyLenBytes)
	offset += copy(bytes[offset:], k.key)
	binary.BigEndian.PutUint64(bytes[offset:], math.MaxUint64-k.height)
	return bytes
}

func (k *keyInternal) Height() uint64 {
	return k.height
}

func (k *keyInternal) InnerKey() []byte {
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

// Takes a slice of bytes and returns a Key struct
func parseRawDBKey(rawKey []byte) (dbKey, error) {
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

	return &keyInternal{
		height: math.MaxUint64 - binary.BigEndian.Uint64(rawKey[rawKeyLen-internalKeySuffixLen:]),
		key:    key,
	}, nil
}
