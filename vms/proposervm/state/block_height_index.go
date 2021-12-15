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
	cacheSize = 8192
)

var (
	_ HeightIndex = &innerHeightIndex{}

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

	SetLatestPreForkHeight(height uint64) error
	GetLatestPreForkHeight() (uint64, error)
	DeleteLatestPreForkHeight() error

	SetRepairCheckpoint(blkID ids.ID) error
	GetRepairCheckpoint() (ids.ID, error)
	DeleteRepairCheckpoint() error

	clearCache() // useful in testing
}

type innerHeightIndex struct {
	// Caches block height -> proposerVMBlockID. If the proposerVMBlockID is nil,
	// the height is not in storage.
	cache cache.Cacher

	db database.Database
}

func NewHeightIndex(db database.Database) HeightIndex {
	return &innerHeightIndex{
		cache: &cache.LRU{Size: cacheSize},
		db:    db,
	}
}

func (ihi *innerHeightIndex) SetBlockIDAtHeight(height uint64, blkID ids.ID) (int, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)

	ihi.cache.Put(string(key), blkID)
	return len(key) + len(blkID), ihi.db.Put(key, blkID[:])
}

func (ihi *innerHeightIndex) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)

	if blkIDIntf, found := ihi.cache.Get(string(key)); found {
		if blkIDIntf == nil {
			return ids.Empty, database.ErrNotFound
		}

		res, _ := blkIDIntf.(ids.ID)
		return res, nil
	}

	bytes, err := ihi.db.Get(key)
	switch err {
	case nil:
		return ids.FromBytes(bytes), nil

	case database.ErrNotFound:
		ihi.cache.Put(string(key), nil)
		return ids.Empty, database.ErrNotFound

	default:
		return ids.Empty, err
	}
}

func (ihi *innerHeightIndex) DeleteBlockIDAtHeight(height uint64) error {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)

	ihi.cache.Put(string(key), nil)
	return ihi.db.Delete(key)
}

func (ihi *innerHeightIndex) SetLatestPreForkHeight(height uint64) error {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	ihi.cache.Put(string(preForkPrefix), heightBytes)
	return ihi.db.Put(preForkPrefix, heightBytes)
}

func (ihi *innerHeightIndex) GetLatestPreForkHeight() (uint64, error) {
	key := preForkPrefix
	if blkIDIntf, found := ihi.cache.Get(string(key)); found {
		if blkIDIntf == nil {
			return 0, database.ErrNotFound
		}

		heightBytes, _ := blkIDIntf.([]byte)
		res := binary.BigEndian.Uint64(heightBytes)
		return res, nil
	}

	bytes, err := ihi.db.Get(key)
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

func (ihi *innerHeightIndex) DeleteLatestPreForkHeight() error {
	key := preForkPrefix
	ihi.cache.Evict(string(key))
	return ihi.db.Delete(key)
}

func (ihi *innerHeightIndex) SetRepairCheckpoint(blkID ids.ID) error {
	key := checkpointPrefix
	ihi.cache.Put(string(key), blkID)
	return ihi.db.Put(key, blkID[:])
}

func (ihi *innerHeightIndex) GetRepairCheckpoint() (ids.ID, error) {
	key := checkpointPrefix
	if blkIDIntf, found := ihi.cache.Get(string(key)); found {
		if blkIDIntf == nil {
			return ids.Empty, database.ErrNotFound
		}

		res, _ := blkIDIntf.(ids.ID)
		return res, nil
	}

	bytes, err := ihi.db.Get(key)
	switch err {
	case nil:
		return ids.FromBytes(bytes), nil

	case database.ErrNotFound:
		return ids.Empty, database.ErrNotFound

	default:
		return ids.Empty, err
	}
}

func (ihi *innerHeightIndex) DeleteRepairCheckpoint() error {
	key := checkpointPrefix
	ihi.cache.Evict(string(key))
	return ihi.db.Delete(key)
}

func (ihi *innerHeightIndex) clearCache() {
	ihi.cache.Flush()
}
