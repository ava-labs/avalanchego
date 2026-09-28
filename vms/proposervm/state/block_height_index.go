// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
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

	forkKey = []byte("fork")
)

type HeightIndexGetter interface {
	// GetMinimumHeight return the smallest height of an indexed blockID. If
	// there are no indexed blockIDs, ErrNotFound will be returned.
	GetMinimumHeight() (uint64, error)
	GetBlockIDAtHeight(height uint64) (ids.ID, error)
	// GetBlockIDsAtHeights returns the IDs of the blocks indexed at the
	// heights in [minHeight, maxHeight], ordered by ascending height. Heights
	// without an indexed block are reported as [ids.Empty]. Callers are
	// expected to bound the size of the requested range.
	GetBlockIDsAtHeights(minHeight, maxHeight uint64) ([]ids.ID, error)

	// Fork height is stored when the first post-fork block/option is accepted.
	// Before that, fork height won't be found.
	GetForkHeight() (uint64, error)
}

type HeightIndexWriter interface {
	SetForkHeight(height uint64) error
	SetBlockIDAtHeight(height uint64, blkID ids.ID) error
	DeleteBlockIDAtHeight(height uint64) error
}

// HeightIndex contains mapping of blockHeights to accepted proposer block IDs
// along with some metadata (fork height and checkpoint).
type HeightIndex interface {
	HeightIndexWriter
	HeightIndexGetter
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

		heightsCache: lru.NewCache[uint64, ids.ID](cacheSize),
		heightDB:     prefixdb.New(heightPrefix, db),
		metadataDB:   prefixdb.New(metadataPrefix, db),
	}
}

func (hi *heightIndex) GetMinimumHeight() (uint64, error) {
	it := hi.heightDB.NewIterator()
	defer it.Release()

	if !it.Next() {
		return 0, database.ErrNotFound
	}

	height, err := database.ParseUInt64(it.Key())
	if err != nil {
		return 0, err
	}
	return height, it.Error()
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

func (hi *heightIndex) GetBlockIDsAtHeights(minHeight, maxHeight uint64) ([]ids.ID, error) {
	if minHeight > maxHeight {
		return nil, nil
	}

	// Heights are stored as big-endian keys, so iterating from minHeight
	// visits the requested range in ascending order with a single sequential
	// scan rather than one read per height.
	blkIDs := make([]ids.ID, maxHeight-minHeight+1)
	it := hi.heightDB.NewIteratorWithStart(database.PackUInt64(minHeight))
	defer it.Release()

	for it.Next() {
		height, err := database.ParseUInt64(it.Key())
		if err != nil {
			return nil, err
		}
		if height > maxHeight {
			break
		}

		blkID, err := ids.ToID(it.Value())
		if err != nil {
			return nil, err
		}
		blkIDs[height-minHeight] = blkID
	}
	return blkIDs, it.Error()
}

func (hi *heightIndex) SetBlockIDAtHeight(height uint64, blkID ids.ID) error {
	hi.heightsCache.Put(height, blkID)
	key := database.PackUInt64(height)
	return database.PutID(hi.heightDB, key, blkID)
}

func (hi *heightIndex) DeleteBlockIDAtHeight(height uint64) error {
	hi.heightsCache.Evict(height)
	key := database.PackUInt64(height)
	return hi.heightDB.Delete(key)
}

func (hi *heightIndex) GetForkHeight() (uint64, error) {
	return database.GetUInt64(hi.metadataDB, forkKey)
}

func (hi *heightIndex) SetForkHeight(height uint64) error {
	return database.PutUInt64(hi.metadataDB, forkKey, height)
}
