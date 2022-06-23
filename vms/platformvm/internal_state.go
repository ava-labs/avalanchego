// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ InternalState = &internalStateImpl{}

	blockPrefix = []byte("block")
)

const blockCacheSize = 2048

type InternalState interface {
	state.State

	SetHeight(height uint64)

	GetBlock(blockID ids.ID) (Block, error)
	AddBlock(block Block)

	Abort()
	Commit() error
	CommitBatch() (database.Batch, error)
	Close() error
}

/*
 * VMDB
 * |-. validators
 * | |-. current
 * | | |-. validator
 * | | | '-. list
 * | | |   '-- txID -> uptime + potential reward
 * | | |-. delegator
 * | | | '-. list
 * | | |   '-- txID -> potential reward
 * | | '-. subnetValidator
 * | |   '-. list
 * | |     '-- txID -> nil
 * | |-. pending
 * | | |-. validator
 * | | | '-. list
 * | | |   '-- txID -> nil
 * | | |-. delegator
 * | | | '-. list
 * | | |   '-- txID -> nil
 * | | '-. subnetValidator
 * | |   '-. list
 * | |     '-- txID -> nil
 * | '-. diffs
 * |   '-. height+subnet
 * |     '-. list
 * |       '-- nodeID -> weightChange
 * |-. blocks
 * | '-- blockID -> block bytes
 * |-. txs
 * | '-- txID -> tx bytes + tx status
 * |- rewardUTXOs
 * | '-. txID
 * |   '-. list
 * |     '-- utxoID -> utxo bytes
 * |- utxos
 * | '-- utxoDB
 * |-. subnets
 * | '-. list
 * |   '-- txID -> nil
 * |-. chains
 * | '-. subnetID
 * |   '-. list
 * |     '-- txID -> nil
 * '-. singletons
 *   |-- initializedKey -> nil
 *   |-- timestampKey -> timestamp
 *   |-- currentSupplyKey -> currentSupply
 *   '-- lastAcceptedKey -> lastAccepted
 */
type internalStateImpl struct {
	state.State

	vm     *VM
	baseDB *versiondb.Database

	currentHeight uint64

	addedBlocks map[ids.ID]Block // map of blockID -> Block
	blockCache  cache.Cacher     // cache of blockID -> Block, if the entry is nil, it is not in the database
	blockDB     database.Database
}

type stateBlk struct {
	Blk    []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`
}

func NewState(vm *VM, db database.Database, genesis []byte, metrics prometheus.Registerer) (InternalState, error) {
	blockCache, err := metercacher.New(
		"block_cache",
		metrics,
		&cache.LRU{Size: blockCacheSize},
	)
	if err != nil {
		return nil, err
	}

	baseDB := versiondb.New(db)

	state, err := state.New(
		baseDB,
		metrics,
		&vm.Config,
		vm.ctx,
		vm.localStake,
		vm.totalStake,
		vm.rewards,
	)
	if err != nil {
		return nil, err
	}

	is := &internalStateImpl{
		State:       state,
		vm:          vm,
		baseDB:      baseDB,
		addedBlocks: make(map[ids.ID]Block),
		blockCache:  blockCache,
		blockDB:     prefixdb.New(blockPrefix, baseDB),
	}

	if err := is.sync(genesis); err != nil {
		// Drop any errors on close to return the first error
		_ = is.Close()

		return nil, err
	}
	return is, nil
}

func (st *internalStateImpl) sync(genesis []byte) error {
	shouldInit, err := st.ShouldInit()
	if err != nil {
		return fmt.Errorf(
			"failed to check if the database is initialized: %w",
			err,
		)
	}

	// If the database is empty, create the platform chain anew using the
	// provided genesis state
	if shouldInit {
		if err := st.init(genesis); err != nil {
			return fmt.Errorf(
				"failed to initialize the database: %w",
				err,
			)
		}
	}

	if err := st.Load(); err != nil {
		return fmt.Errorf(
			"failed to load the database state: %w",
			err,
		)
	}
	return nil
}

func (st *internalStateImpl) SetHeight(height uint64) {
	st.currentHeight = height
}

func (st *internalStateImpl) GetBlock(blockID ids.ID) (Block, error) {
	if blk, exists := st.addedBlocks[blockID]; exists {
		return blk, nil
	}
	if blkIntf, cached := st.blockCache.Get(blockID); cached {
		if blkIntf == nil {
			return nil, database.ErrNotFound
		}
		return blkIntf.(Block), nil
	}

	blkBytes, err := st.blockDB.Get(blockID[:])
	if err == database.ErrNotFound {
		st.blockCache.Put(blockID, nil)
		return nil, database.ErrNotFound
	} else if err != nil {
		return nil, err
	}

	blkStatus := stateBlk{}
	if _, err := GenesisCodec.Unmarshal(blkBytes, &blkStatus); err != nil {
		return nil, err
	}

	var blk Block
	if _, err := GenesisCodec.Unmarshal(blkStatus.Blk, &blk); err != nil {
		return nil, err
	}
	if err := blk.initialize(st.vm, blkStatus.Blk, blkStatus.Status, blk); err != nil {
		return nil, err
	}

	st.blockCache.Put(blockID, blk)
	return blk, nil
}

func (st *internalStateImpl) AddBlock(block Block) {
	st.addedBlocks[block.ID()] = block
}

func (st *internalStateImpl) Abort() {
	st.baseDB.Abort()
}

func (st *internalStateImpl) Commit() error {
	defer st.Abort()
	batch, err := st.CommitBatch()
	if err != nil {
		return err
	}
	return batch.Write()
}

func (st *internalStateImpl) CommitBatch() (database.Batch, error) {
	errs := wrappers.Errs{}
	errs.Add(
		st.writeBlocks(),
		st.State.Write(st.currentHeight),
	)
	if errs.Err != nil {
		return nil, errs.Err
	}
	return st.baseDB.CommitBatch()
}

func (st *internalStateImpl) Close() error {
	errs := wrappers.Errs{}
	errs.Add(
		st.blockDB.Close(),
		st.State.Close(),
		st.baseDB.Close(),
	)
	return errs.Err
}

func (st *internalStateImpl) writeBlocks() error {
	for blkID, blk := range st.addedBlocks {
		blkID := blkID

		sblk := stateBlk{
			Blk:    blk.Bytes(),
			Status: blk.Status(),
		}
		btxBytes, err := GenesisCodec.Marshal(txs.Version, &sblk)
		if err != nil {
			return fmt.Errorf("failed to marshal state block: %w", err)
		}

		delete(st.addedBlocks, blkID)
		st.blockCache.Put(blkID, blk)
		if err := st.blockDB.Put(blkID[:], btxBytes); err != nil {
			return fmt.Errorf("failed to write block: %w", err)
		}
	}
	return nil
}

func (st *internalStateImpl) init(genesisBytes []byte) error {
	// Create the genesis block and save it as being accepted (We don't do
	// genesisBlock.Accept() because then it'd look for genesisBlock's
	// non-existent parent)
	genesisID := hashing.ComputeHash256Array(genesisBytes)
	genesisBlock, err := st.vm.newCommitBlock(genesisID, 0, true)
	if err != nil {
		return err
	}
	genesisBlock.status = choices.Accepted
	st.AddBlock(genesisBlock)

	genesisState, err := genesis.ParseState(genesisBytes)
	if err != nil {
		return err
	}
	if err := st.SyncGenesis(genesisBlock.ID(), genesisState); err != nil {
		return err
	}

	if err := st.DoneInit(); err != nil {
		return err
	}

	return st.Commit()
}
