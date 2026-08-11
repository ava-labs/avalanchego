// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"errors"
	"fmt"

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

const (
	blockCacheSize = 64 * units.MiB

	// innerBlockOffset is the byte offset of the block itself within a
	// serialized blockWrapper: a codec version followed by the length prefix of
	// [blockWrapper.Block], which is the wrapper's first serialized field.
	// TestInnerBlockBytes enforces that layout.
	innerBlockOffset = wrappers.ShortLen + wrappers.IntLen
)

var (
	errBlockWrongVersion     = errors.New("wrong version")
	errTruncatedBlockWrapper = errors.New("truncated block wrapper")

	_ BlockState = (*blockState)(nil)
)

type BlockState interface {
	GetBlock(blkID ids.ID) (block.Block, error)
	// GetBlockBytesAndParent returns the serialized block along with the ID of
	// its parent. See [blockState.GetBlockBytesAndParent].
	GetBlockBytesAndParent(blkID ids.ID) ([]byte, ids.ID, error)
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

// GetBlockBytesAndParent returns the serialized block along with the ID of its
// parent.
//
// It is a cheaper alternative to [blockState.GetBlock] for callers that walk a
// chain of blocks but only need to return their bytes. It performs the same
// single database read, but skips decoding the block, computing its ID, and
// parsing its staking certificate.
//
// Unlike [blockState.GetBlock] it does not populate the block cache. This path
// serves historical range scans, which have no reuse within a walk and would
// otherwise evict the recent blocks that consensus depends on.
func (s *blockState) GetBlockBytesAndParent(blkID ids.ID) ([]byte, ids.ID, error) {
	if blk, found := s.blkCache.Get(blkID); found {
		if blk == nil {
			return nil, ids.Empty, database.ErrNotFound
		}
		return blk.Block, blk.block.ParentID(), nil
	}

	blkWrapperBytes, err := s.db.Get(blkID[:])
	if err != nil {
		return nil, ids.Empty, err
	}

	blkBytes, err := innerBlockBytes(blkWrapperBytes)
	if err != nil {
		return nil, ids.Empty, err
	}

	parentID, err := block.ParentID(blkBytes)
	if err != nil {
		return nil, ids.Empty, err
	}
	return blkBytes, parentID, nil
}

// innerBlockBytes returns the [blockWrapper.Block] field of a serialized
// blockWrapper without decoding the wrapper. The returned slice aliases b.
func innerBlockBytes(b []byte) ([]byte, error) {
	if len(b) < innerBlockOffset {
		return nil, fmt.Errorf("%w: got %d bytes, need at least %d", errTruncatedBlockWrapper, len(b), innerBlockOffset)
	}
	if version := binary.BigEndian.Uint16(b); version != CodecVersion {
		return nil, errBlockWrongVersion
	}
	// Computed in uint64 so that a corrupt length cannot overflow into a valid
	// looking offset.
	end := uint64(innerBlockOffset) + uint64(binary.BigEndian.Uint32(b[wrappers.ShortLen:]))
	if end > uint64(len(b)) {
		return nil, fmt.Errorf("%w: block field ends at %d, past the %d bytes available", errTruncatedBlockWrapper, end, len(b))
	}
	return b[innerBlockOffset:end], nil
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
