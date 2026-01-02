// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

const (
	Uint64Size = 8 // bytes
	BoolSize   = 1 // bytes
	BoolFalse  = 0x00
	BoolTrue   = 0x01

	// kvPairOverhead is an estimated overhead for a kv pair in a database.
	kvPairOverhead = 8 // bytes
)

var (
	boolFalseKey = []byte{BoolFalse}
	boolTrueKey  = []byte{BoolTrue}

	errWrongSize = errors.New("value has unexpected size")
)

func PutID(db KeyValueWriter, key []byte, val ids.ID) error {
	return db.Put(key, val[:])
}

func GetID(db KeyValueReader, key []byte) (ids.ID, error) {
	b, err := db.Get(key)
	if err != nil {
		return ids.Empty, err
	}
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
	bytes := make([]byte, Uint64Size)
	binary.BigEndian.PutUint64(bytes, val)
	return bytes
}

func ParseUInt64(b []byte) (uint64, error) {
	if len(b) != Uint64Size {
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
		return db.Put(key, boolTrueKey)
	}
	return db.Put(key, boolFalseKey)
}

func GetBool(db KeyValueReader, key []byte) (bool, error) {
	b, err := db.Get(key)
	switch {
	case err != nil:
		return false, err
	case len(b) != BoolSize:
		return false, fmt.Errorf("length should be %d but is %d", BoolSize, len(b))
	case b[0] != BoolFalse && b[0] != BoolTrue:
		return false, fmt.Errorf("should be %d or %d but is %d", BoolFalse, BoolTrue, b[0])
	}
	return b[0] == BoolTrue, nil
}

// WithDefault returns the value at [key] in [db]. If the key doesn't exist, it
// returns [def].
func WithDefault[V any](
	get func(KeyValueReader, []byte) (V, error),
	db KeyValueReader,
	key []byte,
	def V,
) (V, error) {
	v, err := get(db, key)
	if err == ErrNotFound {
		return def, nil
	}
	return v, err
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

func AtomicClear(readerDB Iteratee, deleterDB KeyValueDeleter) error {
	return AtomicClearPrefix(readerDB, deleterDB, nil)
}

// AtomicClearPrefix deletes from [deleterDB] all keys in [readerDB] that have the given [prefix].
func AtomicClearPrefix(readerDB Iteratee, deleterDB KeyValueDeleter, prefix []byte) error {
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

// Remove all key-value pairs from [db].
// Writes each batch when it reaches [writeSize].
func Clear(db Database, writeSize int) error {
	return ClearPrefix(db, nil, writeSize)
}

// Removes all keys with the given [prefix] from [db].
// Writes each batch when it reaches [writeSize].
func ClearPrefix(db Database, prefix []byte, writeSize int) error {
	b := db.NewBatch()
	it := db.NewIteratorWithPrefix(prefix)
	// Defer the release of the iterator inside a closure to guarantee that the
	// latest, not the first, iterator is released on return.
	defer func() {
		it.Release()
	}()

	for it.Next() {
		key := it.Key()
		if err := b.Delete(key); err != nil {
			return err
		}

		// Avoid too much memory pressure by periodically writing to the
		// database.
		if b.Size() < writeSize {
			continue
		}

		if err := b.Write(); err != nil {
			return err
		}
		b.Reset()

		// Reset the iterator to release references to now deleted keys.
		if err := it.Error(); err != nil {
			return err
		}
		it.Release()
		it = db.NewIteratorWithPrefix(prefix)
	}

	if err := b.Write(); err != nil {
		return err
	}
	return it.Error()
}
