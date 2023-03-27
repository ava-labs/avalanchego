// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
)

const cacheSize = 8192 // max cache entries

var (
	_ HeightIndex = (*heightIndex)(nil)

	heightPrefix   = []byte("height")
	metadataPrefix = []byte("metadata")

	forkKey       = []byte("fork")
	checkpointKey = []byte("checkpoint")
)

type HeightIndexGetter interface {
	GetBlockIDAtHeight(height uint64) (ids.ID, error)

	// Fork height is stored when the first post-fork block/option is accepted.
	// Before that, fork height won't be found.
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
	versiondb.Commitable

	GetCheckpoint() (ids.ID, error)
	SetCheckpoint(blkID ids.ID) error
	DeleteCheckpoint() error
}

// HeightIndex contains mapping of blockHeights to accepted proposer block IDs
// along with some metadata (fork height and checkpoint).
type HeightIndex interface {
	HeightIndexWriter
	HeightIndexGetter
	HeightIndexBatchSupport
}

type heightIndex struct {
	versiondb.Commitable

	// Caches block height -> proposerVMBlockID.
	heightsCache cache.Cacher[uint64, ids.ID]

	heightDB   database.Database
	metadataDB database.Database
}

func NewHeightIndex(db database.Database, commitable versiondb.Commitable) HeightIndex {
	return &heightIndex{
		Commitable: commitable,

		heightsCache: &cache.LRU[uint64, ids.ID]{Size: cacheSize},
		heightDB:     prefixdb.New(heightPrefix, db),
		metadataDB:   prefixdb.New(metadataPrefix, db),
	}
}

func (hi *heightIndex) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if blkID, found := hi.heightsCache.Get(height); found {
		return blkID, nil
	}

	key := database.PackUInt64(height)
	blkID, err := database.GetID(hi.heightDB, key)
	if err != nil {
		return ids.Empty, err
	}
	hi.heightsCache.Put(height, blkID)
	return blkID, err
}

func (hi *heightIndex) SetBlockIDAtHeight(height uint64, blkID ids.ID) error {
	hi.heightsCache.Put(height, blkID)
	key := database.PackUInt64(height)
	return database.PutID(hi.heightDB, key, blkID)
}

func (hi *heightIndex) GetForkHeight() (uint64, error) {
	return database.GetUInt64(hi.metadataDB, forkKey)
}

func (hi *heightIndex) SetForkHeight(height uint64) error {
	return database.PutUInt64(hi.metadataDB, forkKey, height)
}

func (hi *heightIndex) GetCheckpoint() (ids.ID, error) {
	return database.GetID(hi.metadataDB, checkpointKey)
}

func (hi *heightIndex) SetCheckpoint(blkID ids.ID) error {
	return database.PutID(hi.metadataDB, checkpointKey, blkID)
}

func (hi *heightIndex) DeleteCheckpoint() error {
	return hi.metadataDB.Delete(checkpointKey)
}
