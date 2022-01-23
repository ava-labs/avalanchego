// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var errWrongSize = errors.New("value has unexpected size")

const (
	// kvPairOverhead is an estimated overhead for a kv pair in a database.
	kvPairOverhead = 8 // bytes
)

func PutID(db KeyValueWriter, key []byte, val ids.ID) error {
	return db.Put(key, val[:])
}

func GetID(db KeyValueReader, key []byte) (ids.ID, error) {
	b, err := db.Get(key)
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ToID(b)
}

func ParseID(b []byte) (ids.ID, error) {
	return ids.ToID(b)
}

func PutUInt64(db KeyValueWriter, key []byte, val uint64) error {
	b := PackUInt64(val)
	return db.Put(key, b)
}

func GetUInt64(db KeyValueReader, key []byte) (uint64, error) {
	b, err := db.Get(key)
	if err != nil {
		return 0, err
	}
	return ParseUInt64(b)
}

func PackUInt64(val uint64) []byte {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, val)
	return bytes
}

func ParseUInt64(b []byte) (uint64, error) {
	if len(b) != 8 {
		return 0, errWrongSize
	}
	return binary.BigEndian.Uint64(b), nil
}

func PutUInt32(db KeyValueWriter, key []byte, val uint32) error {
	b := PackUInt32(val)
	return db.Put(key, b)
}

func GetUInt32(db KeyValueReader, key []byte) (uint32, error) {
	b, err := db.Get(key)
	if err != nil {
		return 0, err
	}
	return ParseUInt32(b)
}

func PackUInt32(val uint32) []byte {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, val)
	return bytes
}

func ParseUInt32(b []byte) (uint32, error) {
	if len(b) != 4 {
		return 0, errWrongSize
	}
	return binary.BigEndian.Uint32(b), nil
}

func PutTimestamp(db KeyValueWriter, key []byte, val time.Time) error {
	valBytes, err := val.MarshalBinary()
	if err != nil {
		return err
	}
	return db.Put(key, valBytes)
}

func GetTimestamp(db KeyValueReader, key []byte) (time.Time, error) {
	b, err := db.Get(key)
	if err != nil {
		return time.Time{}, err
	}
	return ParseTimestamp(b)
}

func ParseTimestamp(b []byte) (time.Time, error) {
	val := time.Time{}
	if err := val.UnmarshalBinary(b); err != nil {
		return time.Time{}, err
	}
	return val, nil
}

func PutBool(db KeyValueWriter, key []byte, b bool) error {
	if b {
		return db.Put(key, []byte{1})
	}
	return db.Put(key, []byte{0})
}

func GetBool(db KeyValueReader, key []byte) (bool, error) {
	b, err := db.Get(key)
	switch {
	case err != nil:
		return false, err
	case len(b) != 1:
		return false, fmt.Errorf("length should be 1 but is %d", len(b))
	case b[0] != 0 && b[0] != 1:
		return false, fmt.Errorf("should be 0 or 1 but is %v", b[0])
	}
	return b[0] == 1, nil
}

func Count(db Iteratee) (int, error) {
	iterator := db.NewIterator()
	defer iterator.Release()

	count := 0
	for iterator.Next() {
		count++
	}
	return count, iterator.Error()
}

func Size(db Iteratee) (int, error) {
	iterator := db.NewIterator()
	defer iterator.Release()

	size := 0
	for iterator.Next() {
		size += len(iterator.Key()) + len(iterator.Value()) + kvPairOverhead
	}
	return size, iterator.Error()
}

func Clear(readerDB Iteratee, deleterDB KeyValueDeleter) error {
	return ClearPrefix(readerDB, deleterDB, nil)
}

func ClearPrefix(readerDB Iteratee, deleterDB KeyValueDeleter, prefix []byte) error {
	iterator := readerDB.NewIteratorWithPrefix(prefix)
	defer iterator.Release()

	for iterator.Next() {
		key := iterator.Key()
		if err := deleterDB.Delete(key); err != nil {
			return err
		}
	}
	return iterator.Error()
}
