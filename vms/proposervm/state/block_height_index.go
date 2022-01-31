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
	cacheSize = 8192 // max cache entries
)

var (
	_ HeightIndex = &heightIndex{}

	ForkKey       = []byte("fork")
	CheckpointKey = []byte("checkpoint")
	heightPrefix  = []byte("height")
)

type HeightIndexGetter interface {
	GetBlockIDAtHeight(height uint64) (ids.ID, error)
	GetForkHeight() (uint64, error)
}

type HeightIndexWriter interface {
	SetBlockIDAtHeight(height uint64, blkID ids.ID) error
	SetForkHeight(height uint64) error
}

// A checkpoint is the blockID of the next block to be considered
// for height indexing. We store checkpoints to be able to duly resume
// long-running re-indexing ops.
type HeightIndexBatchSupport interface {
	NewBatch() database.Batch
	GetCheckpoint() (ids.ID, error)
	SetCheckpoint(blkID ids.ID) error
}

// HeightIndex contains mapping of blockHeights to accepted proposer block IDs
// along with some metadata (fork height and checkpoint).
type HeightIndex interface {
	HeightIndexWriter
	HeightIndexGetter
	HeightIndexBatchSupport
}

type heightIndex struct {
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

	blkID, err := database.GetID(hi.db, key)
	if err != nil {
		return ids.Empty, err
	}
	hi.blkHeightsCache.Put(string(key), blkID)
	return blkID, err
}

// GetForkHeight implements HeightIndexGetter
func (hi *heightIndex) GetForkHeight() (uint64, error) {
	return database.GetUInt64(hi.db, ForkKey)
}

// SetBlockIDAtHeight implements HeightIndexWriterDeleter
func (hi *heightIndex) SetBlockIDAtHeight(height uint64, blkID ids.ID) error {
	key := GetEntryKey(height)
	hi.blkHeightsCache.Put(string(key), blkID)
	return database.PutID(hi.db, key, blkID)
}

// SetForkHeight implements HeightIndexWriterDeleter
func (hi *heightIndex) SetForkHeight(height uint64) error {
	return database.PutUInt64(hi.db, ForkKey, height)
}

// GetBatch implements HeightIndexBatchSupport
func (hi *heightIndex) NewBatch() database.Batch {
	return hi.db.NewBatch()
}

// SetCheckpoint implements HeightIndexBatchSupport
func (hi *heightIndex) SetCheckpoint(blkID ids.ID) error {
	return database.PutID(hi.db, CheckpointKey, blkID)
}

// GetCheckpoint implements HeightIndexBatchSupport
func (hi *heightIndex) GetCheckpoint() (ids.ID, error) {
	return database.GetID(hi.db, CheckpointKey)
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
