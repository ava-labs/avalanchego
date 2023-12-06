// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"encoding/binary"
)

func PutUInt64Slice(db KeyValueWriter, key []byte, val []uint64) error {
	b := PackUInt64Slice(val)
	return db.Put(key, b)
}

func GetUInt64Slice(db KeyValueReader, key []byte) ([]uint64, error) {
	b, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	return ParseUInt64Slice(b)
}

func PackUInt64Slice(val []uint64) []byte {
	bytes := make([]byte, Uint64Size*len(val))
	for i := 0; i < len(val); i++ {
		binary.BigEndian.PutUint64(bytes[Uint64Size*i:Uint64Size*(i+1)], val[i])
	}
	return bytes
}

func ParseUInt64Slice(b []byte) ([]uint64, error) {
	n := len(b) / Uint64Size
	if len(b) != n*Uint64Size {
		return nil, errWrongSize
	}
	slice := make([]uint64, n)
	for i := 0; i < n; i++ {
		slice[i] = binary.BigEndian.Uint64(b[Uint64Size*i : Uint64Size*(i+1)])
	}
	return slice, nil
}
