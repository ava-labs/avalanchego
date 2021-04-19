// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errWrongSize = errors.New("value has unexpected size")
)

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
	p := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen)}
	p.PackLong(val)
	return db.Put(key, p.Bytes)
}

func GetUInt64(db KeyValueReader, key []byte) (uint64, error) {
	b, err := db.Get(key)
	if err != nil {
		return 0, err
	}
	return ParseUInt64(b)
}

func ParseUInt64(b []byte) (uint64, error) {
	if len(b) != wrappers.LongLen {
		return 0, errWrongSize
	}
	p := wrappers.Packer{Bytes: b}
	return p.UnpackLong(), nil
}

func PutUInt32(db KeyValueWriter, key []byte, val uint32) error {
	p := wrappers.Packer{Bytes: make([]byte, wrappers.IntLen)}
	p.PackInt(val)
	return db.Put(key, p.Bytes)
}

func GetUInt32(db KeyValueReader, key []byte) (uint32, error) {
	b, err := db.Get(key)
	if err != nil {
		return 0, err
	}
	return ParseUInt32(b)
}

func ParseUInt32(b []byte) (uint32, error) {
	if len(b) != wrappers.IntLen {
		return 0, errWrongSize
	}
	p := wrappers.Packer{Bytes: b}
	return p.UnpackInt(), nil
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

func Size(db Iteratee) (int, error) {
	iterator := db.NewIterator()
	defer iterator.Release()

	size := 0
	for iterator.Next() {
		size += len(iterator.Key()) + len(iterator.Value()) + kvPairOverhead
	}
	return size, iterator.Error()
}
