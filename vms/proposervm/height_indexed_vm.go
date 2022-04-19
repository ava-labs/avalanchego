// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// shouldHeightIndexBeRepaired checks if index needs repairing and stores a
// checkpoint if repairing is needed.
//
// vm.ctx.Lock is acquired to avoid interleaving with block acceptance.
func (vm *VM) shouldHeightIndexBeRepaired() (bool, error) {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	_, err := vm.State.GetCheckpoint()
	if err != database.ErrNotFound {
		return true, err
	}

	// no checkpoint. Either index is complete or repair was never attempted.
	// index is complete iff lastAcceptedBlock is indexed
	latestProBlkID, err := vm.State.GetLastAccepted()
	if err == database.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	lastAcceptedBlk, err := vm.getPostForkBlock(latestProBlkID)
	if err != nil {
		// Could not retrieve last accepted block.
		return false, err
	}

	_, err = vm.State.GetBlockIDAtHeight(lastAcceptedBlk.Height())
	if err != database.ErrNotFound {
		return false, err
	}

	// Index needs repairing. Mark the checkpoint so that, in case new blocks
	// are accepted after the lock is released here but before indexing has
	// started, we do not miss rebuilding the full index.
	return true, vm.State.SetCheckpoint(latestProBlkID)
}

// vm.ctx.Lock should be held
func (vm *VM) VerifyHeightIndex() error {
	if _, ok := vm.ChainVM.(block.HeightIndexedChainVM); !ok {
		return block.ErrHeightIndexedVMNotImplemented
	}

	if !vm.hIndexer.IsRepaired() {
		return block.ErrIndexIncomplete
	}
	return nil
}

// vm.ctx.Lock should be held
func (vm *VM) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if !vm.hIndexer.IsRepaired() {
		return ids.Empty, block.ErrIndexIncomplete
	}

	// The indexer will only report that the index has been repaired if the
	// underlying VM supports indexing.
	innerHVM := vm.ChainVM.(block.HeightIndexedChainVM)
	switch forkHeight, err := vm.State.GetForkHeight(); err {
	case nil:
		if height < forkHeight {
			return innerHVM.GetBlockIDAtHeight(height)
		}
		return vm.State.GetBlockIDAtHeight(height)

	case database.ErrNotFound:
		// fork not reached yet. Block must be pre-fork
		return innerHVM.GetBlockIDAtHeight(height)

	default:
		return ids.Empty, err
	}
}

// As postFork blocks/options are accepted, height index is updated even if its
// repairing is ongoing. vm.ctx.Lock should be held
func (vm *VM) updateHeightIndex(height uint64, blkID ids.ID) error {
	if vm.resetHeightIndexOngoing.GetValue() {
		return nil
	}

	_, err := vm.State.GetCheckpoint()
	switch err {
	case nil:
		// Index rebuilding is ongoing. We can update the index with the current
		// block.

	case database.ErrNotFound:
		// No checkpoint means indexing has either not started or is already
		// done.
		if !vm.hIndexer.IsRepaired() {
			return nil
		}

		// Indexing must have finished. We can update the index with the current
		// block.

	default:
		return fmt.Errorf("failed to load index checkpoint: %w", err)
	}
	return vm.storeHeightEntry(height, blkID)
}

func (vm *VM) storeHeightEntry(height uint64, blkID ids.ID) error {
	switch _, err := vm.State.GetForkHeight(); err {
	case nil:
		// The fork was already reached. Just update the index.

	case database.ErrNotFound:
		// This is the first post fork block, store the fork height.
		if err := vm.State.SetForkHeight(height); err != nil {
			return fmt.Errorf("failed storing fork height: %w", err)
		}

	default:
		return fmt.Errorf("failed to load fork height: %w", err)
	}

	vm.ctx.Log.Debug("indexed block %s at height %d", blkID, height)
	return vm.State.SetBlockIDAtHeight(height, blkID)
}
