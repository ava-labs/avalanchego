// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	_ BlkState = &state{}

	blockPrefix = []byte("block")
)

const blockCacheSize = 2048

type BlkState interface {
	Content
	Management
}

// Content interface collects all methods to query and mutate
// all of the stored blocks.
type Content interface {
	GetStatelessBlock(blockID ids.ID) (stateless.CommonBlockIntf, choices.Status, error)
	AddStatelessBlock(block stateless.CommonBlockIntf, status choices.Status)
}

// Management interface collects all methods used to initialize
// blocks db upon vm initialization, along with methods to
// persist updated state.
type Management interface {
	SyncGenesis(genesisBytes []byte) (ids.ID, error)
	WriteBlocks() error
	CloseBlocks() error
}

func NewState(baseDB *versiondb.Database) BlkState {
	return &state{
		addedBlocks: make(map[ids.ID]stateBlk),
		blockCache:  &cache.LRU{Size: blockCacheSize},
		blockDB:     prefixdb.New(blockPrefix, baseDB),
	}
}

func NewMeteredState(baseDB *versiondb.Database, metrics prometheus.Registerer) (BlkState, error) {
	blockCache, err := metercacher.New(
		"block_cache",
		metrics,
		&cache.LRU{Size: blockCacheSize},
	)
	if err != nil {
		return nil, err
	}

	return &state{
		addedBlocks: make(map[ids.ID]stateBlk),
		blockCache:  blockCache,
		blockDB:     prefixdb.New(blockPrefix, baseDB),
	}, nil
}

type state struct {
	addedBlocks map[ids.ID]stateBlk // map of blockID -> stateBlk
	blockCache  cache.Cacher        // cache of blockID -> Block, if the entry is nil, it is not in the database
	blockDB     database.Database
}

type stateBlk struct {
	Blk    stateless.CommonBlockIntf
	Bytes  []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`
}

func (st *state) GetStatelessBlock(blockID ids.ID) (stateless.CommonBlockIntf, choices.Status, error) {
	if blkState, exists := st.addedBlocks[blockID]; exists {
		return blkState.Blk, blkState.Status, nil
	}
	if blkIntf, cached := st.blockCache.Get(blockID); cached {
		if blkIntf == nil {
			return nil, choices.Processing, database.ErrNotFound // status does not matter here
		}

		blkState := blkIntf.(stateBlk)
		return blkState.Blk, blkState.Status, nil
	}

	blkBytes, err := st.blockDB.Get(blockID[:])
	if err == database.ErrNotFound {
		st.blockCache.Put(blockID, nil)
		return nil, choices.Processing, database.ErrNotFound // status does not matter here
	} else if err != nil {
		return nil, choices.Processing, err // status does not matter here
	}

	blkState := stateBlk{}
	if _, err := stateless.Codec.Unmarshal(blkBytes, &blkState); err != nil {
		return nil, choices.Processing, err // status does not matter here
	}

	statelessBlk, err := stateless.Parse(blkState.Bytes)
	if err != nil {
		return nil, choices.Processing, err // status does not matter here
	}
	blkState.Blk = statelessBlk

	st.blockCache.Put(blockID, blkState)
	return statelessBlk, blkState.Status, nil
}

func (st *state) AddStatelessBlock(block stateless.CommonBlockIntf, status choices.Status) {
	st.addedBlocks[block.ID()] = stateBlk{
		Blk:    block,
		Bytes:  block.Bytes(),
		Status: status,
	}
}

func (st *state) SyncGenesis(genesisBytes []byte) (ids.ID, error) {
	// Create the genesis block and save it as being accepted (We don't just
	// do genesisBlock.Accept() because then it'd look for genesisBlock's
	// non-existent parent)
	genesisID := hashing.ComputeHash256Array(genesisBytes)
	genesisBlk, err := stateless.NewCommitBlock(
		stateless.PreForkVersion,
		0, // timestamp, not serialized for pre-fork blocks
		genesisID,
		0,
	)
	if err != nil {
		return ids.Empty, err
	}

	st.AddStatelessBlock(genesisBlk, choices.Accepted)
	return genesisBlk.ID(), nil
}

func (st *state) WriteBlocks() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to write blocks with: %w", err)
		}
	}()

	for blkID, stateBlk := range st.addedBlocks {
		var (
			btxBytes []byte
			blkID    = blkID
			sblk     = stateBlk
		)

		btxBytes, err = stateless.Codec.Marshal(stateless.PreForkVersion, &sblk)
		if err != nil {
			return
		}

		delete(st.addedBlocks, blkID)
		st.blockCache.Put(blkID, stateBlk)
		if err = st.blockDB.Put(blkID[:], btxBytes); err != nil {
			return
		}
	}
	return nil
}

func (st *state) CloseBlocks() error {
	return st.blockDB.Close()
}
