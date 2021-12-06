// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const (
	cacheSize = 8192
)

var _ AcceptedPostForkBlockHeightIndex = &innerBlocksMapping{}

// AcceptedPostForkBlockHeightIndex contains mapping of blockHeights to accepted proposer block IDs.
// Only accepted blocks are indexed; moreover only post-fork blocks are indexed.
type AcceptedPostForkBlockHeightIndex interface {
	SetBlocksIDByHeight(height uint64, blkID ids.ID) error
	GetBlockIDByHeight(height uint64) (ids.ID, error)
	DeleteBlockIDByHeight(height uint64) error

	clearCache() // useful for UTs
}

type innerBlocksMapping struct {
	// Caches coreBlockID -> proposerVMBlockID. If the proposerVMBlockID is nil, that means the coreBlockID is not
	// in storage.
	cache cache.Cacher

	db database.Database
}

func NewBlockHeightIndex(db database.Database) AcceptedPostForkBlockHeightIndex {
	return &innerBlocksMapping{
		cache: &cache.LRU{Size: cacheSize},
		db:    db,
	}
}

func (ibm *innerBlocksMapping) SetBlocksIDByHeight(height uint64, blkID ids.ID) error {
	heightBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(heightBytes, height)
	ibm.cache.Put(string(heightBytes), blkID)
	return ibm.db.Put(heightBytes, blkID[:])
}

func (ibm *innerBlocksMapping) GetBlockIDByHeight(height uint64) (ids.ID, error) {
	heightBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(heightBytes, height)

	if blkIDIntf, found := ibm.cache.Get(string(heightBytes)); found {
		if blkIDIntf == nil {
			return ids.Empty, database.ErrNotFound
		}

		res, _ := blkIDIntf.(ids.ID)
		return res, nil
	}

	bytes, err := ibm.db.Get(heightBytes)
	switch err {
	case nil:
		var ba [32]byte
		copy(ba[:], bytes)
		return ids.ID(ba), nil

	case database.ErrNotFound:
		ibm.cache.Put(string(heightBytes), nil)
		return ids.Empty, database.ErrNotFound

	default:
		return ids.Empty, err
	}
}

func (ibm *innerBlocksMapping) DeleteBlockIDByHeight(height uint64) error {
	heightBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(heightBytes, height)

	ibm.cache.Put(string(heightBytes), nil)
	return ibm.db.Delete(heightBytes)
}

func (ibm *innerBlocksMapping) clearCache() {
	ibm.cache.Flush()
}
