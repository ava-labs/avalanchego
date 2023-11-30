// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timestampvm

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

const (
	lastAcceptedByte byte = iota
)

const (
	// maximum block capacity of the cache
	blockCacheSize = 8192
)

// persists lastAccepted block IDs with this key
var lastAcceptedKey = []byte{lastAcceptedByte}

var _ BlockState = &blockState{}

// BlockState defines methods to manage state with Blocks and LastAcceptedIDs.
type BlockState interface {
	GetBlock(blkID ids.ID) (*Block, error)
	PutBlock(blk *Block) error
	GetLastAccepted() (ids.ID, error)
	SetLastAccepted(ids.ID) error
}

// blockState implements BlocksState interface with database and cache.
type blockState struct {
	// cache to store blocks
	blkCache cache.Cacher[ids.ID, *Block]
	// block database
	blockDB      database.Database
	lastAccepted ids.ID

	// vm reference
	vm *VM
}

// blkWrapper wraps the actual blk bytes and status to persist them together
type blkWrapper struct {
	Blk    []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`
}

// NewBlockState returns BlockState with a new cache and given db
func NewBlockState(db database.Database, vm *VM) BlockState {
	return &blockState{
		blkCache: &cache.LRU[ids.ID, *Block]{Size: blockCacheSize},
		blockDB:  db,
		vm:       vm,
	}
}

// GetBlock gets Block from either cache or database
func (s *blockState) GetBlock(blkID ids.ID) (*Block, error) {
	// Check if cache has this blkID
	if blk, cached := s.blkCache.Get(blkID); cached {
		// there is a key but value is nil, so return an error
		if blk == nil {
			return nil, database.ErrNotFound
		}
		// We found it return the block in cache
		return blk, nil
	}

	// get block bytes from db with the blkID key
	wrappedBytes, err := s.blockDB.Get(blkID[:])
	if err != nil {
		// we could not find it in the db, let's cache this blkID with nil value
		// so next time we try to fetch the same key we can return error
		// without hitting the database
		if err == database.ErrNotFound {
			s.blkCache.Put(blkID, nil)
		}
		// could not find the block, return error
		return nil, err
	}

	// first decode/unmarshal the block wrapper so we can have status and block bytes
	blkw := blkWrapper{}
	if _, err := Codec.Unmarshal(wrappedBytes, &blkw); err != nil {
		return nil, err
	}

	// now decode/unmarshal the actual block bytes to block
	blk := &Block{}
	if _, err := Codec.Unmarshal(blkw.Blk, blk); err != nil {
		return nil, err
	}

	// initialize block with block bytes, status and vm
	blk.Initialize(blkw.Blk, blkw.Status, s.vm)

	// put block into cache
	s.blkCache.Put(blkID, blk)

	return blk, nil
}

// PutBlock puts block into both database and cache
func (s *blockState) PutBlock(blk *Block) error {
	// create block wrapper with block bytes and status
	blkw := blkWrapper{
		Blk:    blk.Bytes(),
		Status: blk.Status(),
	}

	// encode block wrapper to its byte representation
	wrappedBytes, err := Codec.Marshal(CodecVersion, &blkw)
	if err != nil {
		return err
	}

	blkID := blk.ID()
	// put actual block to cache, so we can directly fetch it from cache
	s.blkCache.Put(blkID, blk)

	// put wrapped block bytes into database
	return s.blockDB.Put(blkID[:], wrappedBytes)
}

// DeleteBlock deletes block from both cache and database
func (s *blockState) DeleteBlock(blkID ids.ID) error {
	s.blkCache.Put(blkID, nil)
	return s.blockDB.Delete(blkID[:])
}

// GetLastAccepted returns last accepted block ID
func (s *blockState) GetLastAccepted() (ids.ID, error) {
	// check if we already have lastAccepted ID in state memory
	if s.lastAccepted != ids.Empty {
		return s.lastAccepted, nil
	}

	// get lastAccepted bytes from database with the fixed lastAcceptedKey
	lastAcceptedBytes, err := s.blockDB.Get(lastAcceptedKey)
	if err != nil {
		return ids.ID{}, err
	}
	// parse bytes to ID
	lastAccepted, err := ids.ToID(lastAcceptedBytes)
	if err != nil {
		return ids.ID{}, err
	}
	// put lastAccepted ID into memory
	s.lastAccepted = lastAccepted
	return lastAccepted, nil
}

// SetLastAccepted persists lastAccepted ID into both cache and database
func (s *blockState) SetLastAccepted(lastAccepted ids.ID) error {
	// if the ID in memory and the given memory are same don't do anything
	if s.lastAccepted == lastAccepted {
		return nil
	}
	// put lastAccepted ID to memory
	s.lastAccepted = lastAccepted
	// persist lastAccepted ID to database with fixed lastAcceptedKey
	return s.blockDB.Put(lastAcceptedKey, lastAccepted[:])
}
