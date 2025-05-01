// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

const blockCacheSize = 64 * units.MiB

var (
	errBlockWrongVersion = errors.New("wrong version")

	_ BlockState = (*blockState)(nil)
)

type BlockState interface {
	GetBlock(blkID ids.ID) (block.Block, error)
	PutBlock(blk block.Block) error
	DeleteBlock(blkID ids.ID) error
}

type blockState struct {
	// Caches BlockID -> Block. If the Block is nil, that means the block is not
	// in storage.
	blkCache cache.Cacher[ids.ID, *blockWrapper]

	db database.Database
}

type blockWrapper struct {
	Block  []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`

	block block.Block
}

func cachedBlockSize(_ ids.ID, bw *blockWrapper) int {
	if bw == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(bw.Block) + wrappers.IntLen + 2*constants.PointerOverhead
}

func NewBlockState(db database.Database) BlockState {
	return &blockState{
		blkCache: lru.NewSizedCache(blockCacheSize, cachedBlockSize),
		db:       db,
	}
}

func NewMeteredBlockState(db database.Database, namespace string, metrics prometheus.Registerer) (BlockState, error) {
	blkCache, err := metercacher.New[ids.ID, *blockWrapper](
		metric.AppendNamespace(namespace, "block_cache"),
		metrics,
		lru.NewSizedCache(blockCacheSize, cachedBlockSize),
	)

	return &blockState{
		blkCache: blkCache,
		db:       db,
	}, err
}

func (s *blockState) GetBlock(blkID ids.ID) (block.Block, error) {
	if blk, found := s.blkCache.Get(blkID); found {
		if blk == nil {
			return nil, database.ErrNotFound
		}
		return blk.block, nil
	}

	blkWrapperBytes, err := s.db.Get(blkID[:])
	if err == database.ErrNotFound {
		s.blkCache.Put(blkID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	blkWrapper := blockWrapper{}
	parsedVersion, err := Codec.Unmarshal(blkWrapperBytes, &blkWrapper)
	if err != nil {
		return nil, err
	}
	if parsedVersion != CodecVersion {
		return nil, errBlockWrongVersion
	}

	// The key was in the database
	blk, err := block.ParseWithoutVerification(blkWrapper.Block)
	if err != nil {
		return nil, err
	}
	blkWrapper.block = blk

	s.blkCache.Put(blkID, &blkWrapper)
	return blk, nil
}

func (s *blockState) PutBlock(blk block.Block) error {
	blkWrapper := blockWrapper{
		Block:  blk.Bytes(),
		Status: choices.Accepted,
		block:  blk,
	}

	bytes, err := Codec.Marshal(CodecVersion, &blkWrapper)
	if err != nil {
		return err
	}

	blkID := blk.ID()
	s.blkCache.Put(blkID, &blkWrapper)
	return s.db.Put(blkID[:], bytes)
}

func (s *blockState) DeleteBlock(blkID ids.ID) error {
	s.blkCache.Evict(blkID)
	return s.db.Delete(blkID[:])
}
