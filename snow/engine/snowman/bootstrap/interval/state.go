// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
)

const (
	rangePrefixByte byte = iota
	blockPrefixByte

	prefixLen = 1
)

var (
	rangePrefix = []byte{rangePrefixByte}
	blockPrefix = []byte{blockPrefixByte}

	errInvalidKeyLength = errors.New("invalid key length")
)

func GetIntervals(db database.Iteratee) ([]*Interval, error) {
	it := db.NewIteratorWithPrefix(rangePrefix)
	defer it.Release()

	var intervals []*Interval
	for it.Next() {
		dbKey := it.Key()
		if len(dbKey) < prefixLen {
			return nil, errInvalidKeyLength
		}

		rangeKey := dbKey[prefixLen:]
		upperBound, err := database.ParseUInt64(rangeKey)
		if err != nil {
			return nil, err
		}

		value := it.Value()
		lowerBound, err := database.ParseUInt64(value)
		if err != nil {
			return nil, err
		}

		intervals = append(intervals, &Interval{
			LowerBound: lowerBound,
			UpperBound: upperBound,
		})
	}
	return intervals, it.Error()
}

func PutInterval(db database.KeyValueWriter, upperBound uint64, lowerBound uint64) error {
	rangeKey := database.PackUInt64(upperBound)
	return database.PutUInt64(
		db,
		append(rangePrefix, rangeKey...),
		lowerBound,
	)
}

func DeleteInterval(db database.KeyValueDeleter, upperBound uint64) error {
	rangeKey := database.PackUInt64(upperBound)
	return db.Delete(
		append(rangePrefix, rangeKey...),
	)
}

func GetBlock(db database.KeyValueReader, height uint64) ([]byte, error) {
	blockKey := database.PackUInt64(height)
	return db.Get(
		append(blockPrefix, blockKey...),
	)
}

func PutBlock(db database.KeyValueWriter, height uint64, bytes []byte) error {
	blockKey := database.PackUInt64(height)
	return db.Put(
		append(blockPrefix, blockKey...),
		bytes,
	)
}

func DeleteBlock(db database.KeyValueDeleter, height uint64) error {
	blockKey := database.PackUInt64(height)
	return db.Delete(
		append(blockPrefix, blockKey...),
	)
}
