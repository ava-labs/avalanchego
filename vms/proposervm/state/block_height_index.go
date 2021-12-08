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

var (
	_ AcceptedPostForkBlockHeightIndex = &innerBlocksMapping{}

	heightPrefix  = []byte("heightkey")
	preForkPrefix = []byte("preForkKey")
)

// AcceptedPostForkBlockHeightIndex contains mapping of blockHeights to accepted proposer block IDs.
// Only accepted blocks are indexed; moreover only post-fork blocks are indexed.
type AcceptedPostForkBlockHeightIndex interface {
	SetBlocksIDByHeight(height uint64, blkID ids.ID) error
	GetBlockIDByHeight(height uint64) (ids.ID, error)
	DeleteBlockIDByHeight(height uint64) error

	SetLatestPreForkHeight(height uint64) error
	GetLatestPreForkHeight() (uint64, error)
	DeleteLatestPreForkHeight() error

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
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix)+len(heightBytes))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)

	ibm.cache.Put(string(key), blkID)
	return ibm.db.Put(key, blkID[:])
}

func (ibm *innerBlocksMapping) GetBlockIDByHeight(height uint64) (ids.ID, error) {
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix)+len(heightBytes))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)

	if blkIDIntf, found := ibm.cache.Get(string(key)); found {
		if blkIDIntf == nil {
			return ids.Empty, database.ErrNotFound
		}

		res, _ := blkIDIntf.(ids.ID)
		return res, nil
	}

	bytes, err := ibm.db.Get(key)
	switch err {
	case nil:
		var ba [32]byte
		copy(ba[:], bytes)
		return ids.ID(ba), nil

	case database.ErrNotFound:
		ibm.cache.Put(string(key), nil)
		return ids.Empty, database.ErrNotFound

	default:
		return ids.Empty, err
	}
}

func (ibm *innerBlocksMapping) DeleteBlockIDByHeight(height uint64) error {
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, height)
	key := make([]byte, len(heightPrefix)+len(heightBytes))
	copy(key, heightPrefix)
	key = append(key, heightBytes...)

	ibm.cache.Put(string(key), nil)
	return ibm.db.Delete(key)
}

func (ibm *innerBlocksMapping) SetLatestPreForkHeight(height uint64) error {
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, height)

	ibm.cache.Put(string(preForkPrefix), heightBytes)
	return ibm.db.Put(preForkPrefix, heightBytes)
}

func (ibm *innerBlocksMapping) GetLatestPreForkHeight() (uint64, error) {
	key := preForkPrefix

	if blkIDIntf, found := ibm.cache.Get(string(key)); found {
		if blkIDIntf == nil {
			return 0, database.ErrNotFound
		}

		heightBytes, _ := blkIDIntf.([]byte)
		res := binary.BigEndian.Uint64(heightBytes)
		return res, nil
	}

	bytes, err := ibm.db.Get(key)
	switch err {
	case nil:
		res := binary.BigEndian.Uint64(bytes)
		return res, nil

	case database.ErrNotFound:
		ibm.cache.Put(string(key), nil)
		return 0, database.ErrNotFound

	default:
		return 0, err
	}
}

func (ibm *innerBlocksMapping) DeleteLatestPreForkHeight() error {
	key := preForkPrefix
	ibm.cache.Put(string(key), nil)
	return ibm.db.Delete(key)
}

func (ibm *innerBlocksMapping) clearCache() {
	ibm.cache.Flush()
}
