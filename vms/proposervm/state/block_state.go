// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/option"
)

const (
	blockCacheSize = 8192
)

var (
	errWrongVersion = errors.New("wrong version")

	_ BlockState = &blockState{}
)

type BlockState interface {
	GetBlock(blkID ids.ID) (block.Block, choices.Status, error)
	PutBlock(blk block.Block, status choices.Status) error
	DeleteBlock(blkID ids.ID) error

	GetOption(blkID ids.ID) (option.Option, choices.Status, error)
	PutOption(opt option.Option, status choices.Status) error
	DeleteOption(blkID ids.ID) error

	WipeCache() // useful for UTs
}

type blockState struct {
	// Caches BlockID -> Block. If the Block is nil, that means the block is not
	// in storage.
	cache cache.Cacher
	db    database.Database
}

func (s *blockState) WipeCache() {
	s.cache.Flush()
}

type blockWrapper struct {
	Block  []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`

	block block.Block
}

type optionWrapper struct {
	Block  []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`

	option option.Option
}

func NewBlockState(db database.Database) BlockState {
	return &blockState{
		cache: &cache.LRU{Size: blockCacheSize},
		db:    db,
	}
}

func NewMeteredBlockState(db database.Database, namespace string, metrics prometheus.Registerer) (BlockState, error) {
	cache, err := metercacher.New(
		fmt.Sprintf("%s_block_cache", namespace),
		metrics,
		&cache.LRU{Size: blockCacheSize},
	)
	return &blockState{
		cache: cache,
		db:    db,
	}, err
}

func (s *blockState) GetBlock(blkID ids.ID) (block.Block, choices.Status, error) {
	if blkIntf, found := s.cache.Get(blkID); found {
		if blkIntf == nil {
			return nil, choices.Unknown, database.ErrNotFound
		}
		blk, ok := blkIntf.(*blockWrapper)
		if !ok {
			return nil, choices.Unknown, database.ErrNotFound
		}
		return blk.block, blk.Status, nil
	}

	blkWrapperBytes, err := s.db.Get(blkID[:])
	if err == database.ErrNotFound {
		s.cache.Put(blkID, nil)
		return nil, choices.Unknown, database.ErrNotFound
	}
	if err != nil {
		return nil, choices.Unknown, err
	}

	blkWrapper := blockWrapper{}
	parsedVersion, err := c.Unmarshal(blkWrapperBytes, &blkWrapper)
	if err != nil {
		return nil, choices.Unknown, err
	}
	if parsedVersion != version {
		return nil, choices.Unknown, errWrongVersion
	}

	// The key was in the database
	blk, err := block.Parse(blkWrapper.Block)
	if err != nil {
		return nil, choices.Unknown, err
	}
	blkWrapper.block = blk

	s.cache.Put(blkID, &blkWrapper)
	return blk, blkWrapper.Status, nil
}

func (s *blockState) PutBlock(blk block.Block, status choices.Status) error {
	blkWrapper := blockWrapper{
		Block:  blk.Bytes(),
		Status: status,
		block:  blk,
	}

	bytes, err := c.Marshal(version, &blkWrapper)
	if err != nil {
		return err
	}

	blkID := blk.ID()
	s.cache.Put(blkID, &blkWrapper)
	return s.db.Put(blkID[:], bytes)
}

func (s *blockState) DeleteBlock(blkID ids.ID) error {
	s.cache.Put(blkID, nil)
	return s.db.Delete(blkID[:])
}

func (s *blockState) GetOption(blkID ids.ID) (option.Option, choices.Status, error) {
	if optIntf, found := s.cache.Get(blkID); found {
		if optIntf == nil {
			return nil, choices.Unknown, database.ErrNotFound
		}
		opt, ok := optIntf.(*optionWrapper)
		if !ok {
			return nil, choices.Unknown, database.ErrNotFound
		}
		return opt.option, opt.Status, nil
	}

	optWrapperBytes, err := s.db.Get(blkID[:])
	if err == database.ErrNotFound {
		s.cache.Put(blkID, nil)
		return nil, choices.Unknown, database.ErrNotFound
	}
	if err != nil {
		return nil, choices.Unknown, err
	}

	optWrapper := optionWrapper{}
	parsedVersion, err := c.Unmarshal(optWrapperBytes, &optWrapper)
	if err != nil {
		return nil, choices.Unknown, err
	}
	if parsedVersion != version {
		return nil, choices.Unknown, errWrongVersion
	}

	// The key was in the database
	opt, err := option.Parse(optWrapper.Block)
	if err != nil {
		return nil, choices.Unknown, err
	}
	optWrapper.option = opt

	s.cache.Put(blkID, &optWrapper)
	return opt, optWrapper.Status, nil
}

func (s *blockState) PutOption(opt option.Option, status choices.Status) error {
	optWrapper := optionWrapper{
		Block:  opt.Bytes(),
		Status: status,
		option: opt,
	}

	bytes, err := c.Marshal(version, &optWrapper)
	if err != nil {
		return err
	}

	blkID := opt.ID()
	s.cache.Put(blkID, &optWrapper)
	return s.db.Put(blkID[:], bytes)
}

func (s *blockState) DeleteOption(blkID ids.ID) error {
	s.cache.Put(blkID, nil)
	return s.db.Delete(blkID[:])
}
