// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// IsEnabled implements HeightIndexedChainVM interface
// vm.ctx.Lock should be held
func (vm *VM) IsHeightIndexingEnabled() bool {
	innerHVM, ok := vm.ChainVM.(block.HeightIndexedChainVM)
	if !ok {
		return false
	}
	return innerHVM.IsHeightIndexingEnabled()
}

// IsHeightIndexComplete implements HeightIndexedChainVM interface
// vm.ctx.Lock should be held
func (vm *VM) IsHeightIndexComplete() bool {
	return vm.hIndexer.IsRepaired()
}

// GetBlockIDByHeight implements HeightIndexedChainVM interface
// vm.ctx.Lock should be held
func (vm *VM) GetBlockIDByHeight(height uint64) (ids.ID, error) {
	if !vm.hIndexer.IsRepaired() {
		return ids.Empty, block.ErrIndexIncomplete
	}

	innerHVM, _ := vm.ChainVM.(block.HeightIndexedChainVM)
	switch forkHeight, err := vm.State.GetForkHeight(); err {
	case nil:
		if height < forkHeight {
			return innerHVM.GetBlockIDByHeight(height)
		}
		return vm.State.GetBlockIDAtHeight(height)

	case database.ErrNotFound:
		// fork not reached yet. Block must be pre-fork
		return innerHVM.GetBlockIDByHeight(height)

	default:
		return ids.Empty, err
	}
}

// As postFork blocks/options are accepted, height index is updated
// even if its repairing is ongoing.
// updateHeightIndex should not be called for preFork blocks. Moreover
// vm.ctx.Lock should be held
func (vm *VM) updateHeightIndex(height uint64, blkID ids.ID) error {
	checkpoint, err := vm.State.GetCheckpoint()
	switch err {
	case nil:
		// index rebuilding is ongoing. We can update the index with current block
		// except if it is checkpointed blk, which will be handled by indexer.
		if blkID != checkpoint {
			return vm.storeHeightEntry(height, blkID)
		}

	case database.ErrNotFound:
		// no checkpoint means indexing is not started or is already done
		if vm.hIndexer.IsRepaired() {
			return vm.storeHeightEntry(height, blkID)
		}

	default:
		return err
	}
	return nil
}

func (vm *VM) storeHeightEntry(height uint64, blkID ids.ID) error {
	switch _, err := vm.State.GetForkHeight(); err {
	case nil:
		// fork already reached. Just update the index

	case database.ErrNotFound:
		// this is the first post Fork block/option, store fork height
		if err := vm.State.SetForkHeight(height); err != nil {
			vm.ctx.Log.Warn("Block indexing by height: new block. Failed storing fork height %v", err)
			return err
		}

	default:
		vm.ctx.Log.Warn("Block indexing by height: new block. Could not load fork height %v", err)
		return err
	}

	return vm.State.SetBlockIDAtHeight(height, blkID)
}
