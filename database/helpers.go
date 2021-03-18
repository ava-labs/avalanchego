// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errWrongSize = errors.New("value has unexpected size")
)

func PutID(db KeyValueWriter, key []byte, val ids.ID) error {
	return db.Put(key, val[:])
}
func GetID(db KeyValueReader, key []byte) (ids.ID, error) {
	bytes, err := db.Get(key)
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ToID(bytes)
}

func PutUInt64(db KeyValueWriter, key []byte, val uint64) error {
	p := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen)}
	p.PackLong(val)
	return db.Put(key, p.Bytes)
}
func GetUInt64(db KeyValueReader, key []byte) (uint64, error) {
	bytes, err := db.Get(key)
	if err != nil {
		return 0, err
	}
	if len(bytes) != wrappers.LongLen {
		return 0, errWrongSize
	}
	p := wrappers.Packer{Bytes: bytes}
	return p.UnpackLong(), nil
}

func PutUInt32(db KeyValueWriter, key []byte, val uint32) error {
	p := wrappers.Packer{Bytes: make([]byte, wrappers.IntLen)}
	p.PackInt(val)
	return db.Put(key, p.Bytes)
}
func GetUInt32(db KeyValueReader, key []byte) (uint32, error) {
	bytes, err := db.Get(key)
	if err != nil {
		return 0, err
	}
	if len(bytes) != wrappers.IntLen {
		return 0, errWrongSize
	}
	p := wrappers.Packer{Bytes: bytes}
	return p.UnpackInt(), nil
}
