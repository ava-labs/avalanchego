// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var errIndexIncomplete = errors.New("query failed since because height index is incomplete")

// HeightIndexingEnabled implements HeightIndexedChainVM interface
// vm.ctx.Lock should be held
func (vm *VM) IsHeightIndexComplete() bool {
	innerHVM, ok := vm.ChainVM.(block.HeightIndexedChainVM)
	if !ok || !innerHVM.IsHeightIndexComplete() {
		return false
	}

	return vm.HeightIndexer.IsRepaired()
}

// GetBlockIDByHeight implements HeightIndexedChainVM interface
// vm.ctx.Lock should be held
func (vm *VM) GetBlockIDByHeight(height uint64) (ids.ID, error) {
	innerHVM, ok := vm.ChainVM.(block.HeightIndexedChainVM)
	if !ok {
		return ids.Empty, block.ErrHeightIndexedVMNotImplemented
	}
	if !innerHVM.IsHeightIndexComplete() {
		return ids.Empty, errIndexIncomplete
	}

	// preFork blocks are indexed in innerVM only
	forkHeight, err := vm.State.GetForkHeight()
	if err != nil {
		return ids.Empty, err
	}

	if height < forkHeight {
		return innerHVM.GetBlockIDByHeight(height)
	}

	// postFork blocks are indexed in proposerVM
	return vm.State.GetBlockIDAtHeight(height)
}

// As postFork blocks/options are accepted, height index is updated
// even if its repairing is ongoing.
// updateHeightIndex should not be called for preFork blocks. Morever
// vm.ctx.Lock should be held
func (vm *VM) updateHeightIndex(height uint64, blkID ids.ID) error {
	innerHVM, ok := vm.ChainVM.(block.HeightIndexedChainVM)
	if !ok || !innerHVM.IsHeightIndexComplete() {
		return nil // nothing to do
	}

	forkHeight, err := vm.State.GetForkHeight()
	if err != nil {
		return err
	}

	if forkHeight > height {
		vm.ctx.Log.Info("Block indexing by height: new block. Moved fork height from %d to %d with block %v",
			forkHeight, height, blkID)

		if err := vm.State.SetForkHeight(height); err != nil {
			vm.ctx.Log.Info("Block indexing by height: new block. Failed storing new fork height %v", err)
			return err
		}
	}

	_, err = vm.State.SetBlockIDAtHeight(height, blkID)
	return err
}
