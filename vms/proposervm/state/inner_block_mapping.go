// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const (
	cacheSize = 8192
)

var _ InnerBlocksMapping = &innerBlocksMapping{}

// Store mapping of confirmed inner block ID to the wrapping proposer block ID
// Note that this should be done on accepted blocks only since a not-yet-accepted
// core block can be wrapped into multiple proposer blocks.
type InnerBlocksMapping interface {
	GetWrappingBlockID(coreblkID ids.ID) (ids.ID, error)
	SetBlocksIDMapping(coreblkID, blkID ids.ID) error
	DeleteBlocksIDMapping(coreblkID ids.ID) error

	clearCache() // useful for UTs
}

type innerBlocksMapping struct {
	// Caches coreBlockID -> proposerVMBlockID. If the proposerVMBlockID is nil, that means the coreBlockID is not
	// in storage.
	cache cache.Cacher

	db database.Database
}

func NewInnerBlocksMapping(db database.Database) InnerBlocksMapping {
	return &innerBlocksMapping{
		cache: &cache.LRU{Size: cacheSize},
		db:    db,
	}
}

func (ibm *innerBlocksMapping) GetWrappingBlockID(coreblkID ids.ID) (ids.ID, error) {
	if blkIDIntf, found := ibm.cache.Get(coreblkID); found {
		if blkIDIntf == nil {
			return ids.Empty, database.ErrNotFound
		}

		res, _ := blkIDIntf.(ids.ID)
		return res, nil
	}

	bytes, err := ibm.db.Get(coreblkID[:])
	switch err {
	case nil:
		var ba [32]byte
		copy(ba[:], bytes)
		return ids.ID(ba), nil

	case database.ErrNotFound:
		ibm.cache.Put(coreblkID, nil)
		return ids.Empty, database.ErrNotFound

	default:
		return ids.Empty, err
	}
}

func (ibm *innerBlocksMapping) SetBlocksIDMapping(coreblkID, blkID ids.ID) error {
	ibm.cache.Put(coreblkID, blkID)
	return ibm.db.Put(coreblkID[:], blkID[:])
}

func (ibm *innerBlocksMapping) DeleteBlocksIDMapping(coreblkID ids.ID) error {
	ibm.cache.Put(coreblkID, nil)
	return ibm.db.Delete(coreblkID[:])
}

func (ibm *innerBlocksMapping) clearCache() {
	ibm.cache.Flush()
}
