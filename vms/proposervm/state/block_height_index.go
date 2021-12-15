// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	cacheSize = 8192 // bytes
)

var (
	_ HeightIndex = &heightIndex{}

	heightPrefix     = []byte("heightkey")
	preForkPrefix    = []byte("preForkKey")
	checkpointPrefix = []byte("checkpoint")
)

// HeightIndex contains mapping of blockHeights to accepted proposer block IDs.
// Only accepted blocks are indexed; moreover only post-fork blocks are indexed.
type HeightIndex interface {
	SetBlockIDAtHeight(height uint64, blkID ids.ID) (int, error)
	GetBlockIDAtHeight(height uint64) (ids.ID, error)
	DeleteBlockIDAtHeight(height uint64) error

	SetForkHeight(height uint64) error
	GetForkHeight() (uint64, error)
	DeleteForkHeight() error

	SetCheckpoint(blkID ids.ID) error
	GetCheckpoint() (ids.ID, error)
	DeleteCheckpoint() error

	clearCache() // useful in testing
}

type heightIndex struct {
	// Caches block height -> proposerVMBlockID. If the proposerVMBlockID is nil,
	// the height is not in storage.
	cache cache.Cacher

	db database.Database
}

func NewHeightIndex(db database.Database) HeightIndex {
	return &heightIndex{
		cache: &cache.LRU{Size: cacheSize},
		db:    db,
	}
}

func (hi *heightIndex) SetBlockIDAtHeight(height uint64, blkID ids.ID) (int, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)

	hi.cache.Put(string(key), blkID)
	return len(key) + len(blkID), hi.db.Put(key, blkID[:])
}

func (hi *heightIndex) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)

	if blkIDIntf, found := hi.cache.Get(string(key)); found {
		if blkIDIntf == nil {
			return ids.Empty, database.ErrNotFound
		}

		res, _ := blkIDIntf.(ids.ID)
		return res, nil
	}

	bytes, err := hi.db.Get(key)
	switch err {
	case nil:
		return ids.FromBytes(bytes), nil

	case database.ErrNotFound:
		hi.cache.Put(string(key), nil)
		return ids.Empty, database.ErrNotFound

	default:
		return ids.Empty, err
	}
}

func (hi *heightIndex) DeleteBlockIDAtHeight(height uint64) error {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)

	hi.cache.Put(string(key), nil)
	return hi.db.Delete(key)
}

func (hi *heightIndex) SetForkHeight(height uint64) error {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	hi.cache.Put(string(preForkPrefix), heightBytes)
	return hi.db.Put(preForkPrefix, heightBytes)
}

func (hi *heightIndex) GetForkHeight() (uint64, error) {
	if blkIDIntf, found := hi.cache.Get(string(preForkPrefix)); found {
		if blkIDIntf == nil {
			return 0, database.ErrNotFound
		}

		heightBytes, _ := blkIDIntf.([]byte)
		res := binary.BigEndian.Uint64(heightBytes)
		return res, nil
	}

	bytes, err := hi.db.Get(preForkPrefix)
	switch err {
	case nil:
		res := binary.BigEndian.Uint64(bytes)
		return res, nil

	case database.ErrNotFound:
		return 0, database.ErrNotFound

	default:
		return 0, err
	}
}

func (hi *heightIndex) DeleteForkHeight() error {
	hi.cache.Evict(string(preForkPrefix))
	return hi.db.Delete(preForkPrefix)
}

func (hi *heightIndex) SetCheckpoint(blkID ids.ID) error {
	hi.cache.Put(string(checkpointPrefix), blkID)
	return hi.db.Put(checkpointPrefix, blkID[:])
}

func (hi *heightIndex) GetCheckpoint() (ids.ID, error) {
	if blkIDIntf, found := hi.cache.Get(string(checkpointPrefix)); found {
		if blkIDIntf == nil {
			return ids.Empty, database.ErrNotFound
		}

		res, _ := blkIDIntf.(ids.ID)
		return res, nil
	}

	bytes, err := hi.db.Get(checkpointPrefix)
	switch err {
	case nil:
		return ids.FromBytes(bytes), nil

	case database.ErrNotFound:
		return ids.Empty, database.ErrNotFound

	default:
		return ids.Empty, err
	}
}

func (hi *heightIndex) DeleteCheckpoint() error {
	hi.cache.Evict(string(checkpointPrefix))
	return hi.db.Delete(checkpointPrefix)
}

func (hi *heightIndex) clearCache() {
	hi.cache.Flush()
}
