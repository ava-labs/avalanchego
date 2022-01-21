// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"sync"

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

	heightPrefix = []byte("heightkey")
)

type HeightIndexGetter interface {
	GetBlockIDAtHeight(height uint64) (ids.ID, error)
	GetForkHeight() (uint64, error)
}

type HeightIndexWriterDeleter interface {
	SetBlockIDAtHeight(height uint64, blkID ids.ID) error
	DeleteBlockIDAtHeight(height uint64) error
	SetForkHeight(height uint64) error
	DeleteForkHeight() error
	clearCache() // useful in testing
}

type HeightIndexBatchSupport interface {
	NewBatch() database.Batch
	GetCheckpoint() (ids.ID, error)
	SetCheckpoint(blkID ids.ID) error
	DeleteCheckpoint() error
}

// HeightIndex contains mapping of blockHeights to accepted proposer block IDs
// along with some metadata (fork height and checkpoint).
type HeightIndex interface {
	HeightIndexWriterDeleter
	HeightIndexGetter

	HeightIndexBatchSupport
}

type heightIndex struct {
	// heightIndex may be accessed by a long-running goroutine rebuilding the index
	// as well as main goroutine querying blocks. Hence the lock
	Lock sync.RWMutex

	// Caches block height -> proposerVMBlockID.
	blkHeightsCache cache.Cacher

	db database.Database
}

func NewHeightIndex(db database.Database) HeightIndex {
	return &heightIndex{
		blkHeightsCache: &cache.LRU{Size: cacheSize},
		db:              db,
	}
}

// GetBlockIDAtHeight implements HeightIndexGetter
func (hi *heightIndex) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	key := GetEntryKey(height)
	if blkIDIntf, found := hi.blkHeightsCache.Get(string(key)); found {
		res, _ := blkIDIntf.(ids.ID)
		return res, nil
	}

	bytes, err := hi.db.Get(key)
	switch err {
	case nil:
		res := ids.FromBytes(bytes)
		hi.blkHeightsCache.Put(string(key), res)
		return res, nil

	case database.ErrNotFound:
		return ids.Empty, database.ErrNotFound

	default:
		return ids.Empty, err
	}
}

// GetForkHeight implements HeightIndexGetter
func (hi *heightIndex) GetForkHeight() (uint64, error) {
	switch height, err := database.GetUInt64(hi.db, GetForkKey()); err {
	case nil:
		return height, nil

	case database.ErrNotFound:
		return 0, database.ErrNotFound

	default:
		return 0, err
	}
}

// SetBlockIDAtHeight implements HeightIndexWriterDeleter
func (hi *heightIndex) SetBlockIDAtHeight(height uint64, blkID ids.ID) error {
	key := GetEntryKey(height)
	hi.blkHeightsCache.Put(string(key), blkID)
	return hi.db.Put(key, blkID[:])
}

// DeleteBlockIDAtHeight implements HeightIndexWriterDeleter
func (hi *heightIndex) DeleteBlockIDAtHeight(height uint64) error {
	key := GetEntryKey(height)
	hi.blkHeightsCache.Evict(string(key))
	return hi.db.Delete(key)
}

// SetForkHeight implements HeightIndexWriterDeleter
func (hi *heightIndex) SetForkHeight(height uint64) error {
	return database.PutUInt64(hi.db, GetForkKey(), height)
}

// DeleteForkHeight implements HeightIndexWriterDeleter
func (hi *heightIndex) DeleteForkHeight() error {
	return hi.db.Delete(GetForkKey())
}

// clearCache implements HeightIndexWriterDeleter
func (hi *heightIndex) clearCache() {
	hi.blkHeightsCache.Flush()
}

// GetBatch implements HeightIndexBatchSupport
func (hi *heightIndex) NewBatch() database.Batch { return hi.db.NewBatch() }

// SetCheckpoint implements HeightIndexBatchSupport
func (hi *heightIndex) SetCheckpoint(blkID ids.ID) error {
	return hi.db.Put(GetCheckpointKey(), blkID[:])
}

// GetCheckpoint implements HeightIndexBatchSupport
func (hi *heightIndex) GetCheckpoint() (ids.ID, error) {
	switch bytes, err := hi.db.Get(GetCheckpointKey()); err {
	case nil:
		return ids.FromBytes(bytes), nil
	case database.ErrNotFound:
		return ids.Empty, database.ErrNotFound
	default:
		return ids.Empty, err
	}
}

// DeleteCheckpoint implements HeightIndexBatchSupport
func (hi *heightIndex) DeleteCheckpoint() error {
	return hi.db.Delete(GetCheckpointKey())
}

// helpers functions to create keys/values.
// Currently exported for indexer
func GetEntryKey(height uint64) []byte {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)
	return key
}

func GetForkKey() []byte {
	preForkPrefix := []byte("preForkKey")
	return preForkPrefix
}

func GetCheckpointKey() []byte {
	checkpointPrefix := []byte("checkpoint")
	return checkpointPrefix
}
