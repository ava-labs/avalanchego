// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/units"
)

const commitSizeCap = 1 * units.MiB

var _ block.HeightIndexedChainVM = &VM{}

func (vm *VM) HeightIndexingEnabled() bool {
	hVM, ok := vm.ChainVM.(block.HeightIndexedChainVM)
	if !ok {
		return false
	}

	if !hVM.HeightIndexingEnabled() {
		return false
	}

	// If height indexing is not complete, we mark HeightIndexedChainVM as disabled,
	// even if vm.ChainVM is ready to serve blocks by height
	doRepair, _, err := vm.shouldRepair()
	if doRepair || err != nil {
		return false
	}

	return true
}

func (vm *VM) GetBlockIDByHeight(height uint64) (ids.ID, error) {
	hVM, ok := vm.ChainVM.(block.HeightIndexedChainVM)
	if !ok {
		return ids.Empty, block.ErrHeightIndexedVMNotImplemented
	}

	// preFork blocks are indexed in innerVM only
	if height <= vm.latestPreForkHeight {
		return hVM.GetBlockIDByHeight(height)
	}

	// postFork blocks are indexed in proposerVM
	return vm.State.GetBlkIDByHeight(height)
}

func (vm *VM) updateHeightIndex(height uint64, blkID ids.ID) error {
	if _, ok := vm.ChainVM.(block.HeightIndexedChainVM); !ok {
		// nothing to index if innerVM does not support height indexing
		return nil
	}

	_, err := vm.State.SetBlkIDByHeight(height, blkID)
	return err
}

func (vm *VM) updateLatestPreForkBlockHeight(height uint64) error {
	if _, ok := vm.ChainVM.(block.HeightIndexedChainVM); !ok {
		// nothing to index if innerVM does not support height indexing
		return nil
	}

	if height <= vm.latestPreForkHeight {
		return nil
	}

	vm.latestPreForkHeight = height
	return vm.State.SetLatestPreForkHeight(height)
}

// shouldRepair checks if height index is complete;
// if not, it returns highest un-indexed block ID from which repairing should start.
// shouldRepair should be called synchronously upon VM initialization.
func (vm *VM) shouldRepair() (bool, ids.ID, error) {
	if _, ok := vm.ChainVM.(block.HeightIndexedChainVM); !ok {
		// no index, nothing to repair
		return false, ids.Empty, nil
	}

	repairStartBlkID, err := vm.State.GetRepairCheckpoint()
	switch err {
	case nil:
		// checkpoint found, repair must be resumed
		return true, repairStartBlkID, nil
	case database.ErrNotFound:
		// no checkpoint. Either index is complete or repair was never attempted.
		break
	default:
		return true, ids.Empty, err
	}

	// index is complete iff lastAcceptedBlock is indexed
	latestProBlkID, err := vm.State.GetLastAccepted() // this won't return preFork blks
	switch err {
	case nil:
		break
	case database.ErrNotFound:
		// snowman++ has not forked yet. Mapping is ok;
		// just check latestPreForkHeight is duly set.
		latestInnerBlkID, err := vm.ChainVM.LastAccepted()
		if err != nil {
			return true, ids.Empty, err
		}
		lastInnerBlk, err := vm.ChainVM.GetBlock(latestInnerBlkID)
		if err != nil {
			return true, ids.Empty, err
		}
		vm.latestPreForkHeight = lastInnerBlk.Height()
		if err := vm.State.SetLatestPreForkHeight(vm.latestPreForkHeight); err != nil {
			return true, ids.Empty, err
		}
		return false, ids.Empty, nil
	default:
		return true, ids.Empty, err
	}
	lastAcceptedBlk, err := vm.getPostForkBlock(latestProBlkID)
	if err != nil {
		// Could not retrieve block for LastAccepted Block.
		// We got bigger problems than repairing the index
		return true, ids.Empty, err
	}

	_, err = vm.State.GetBlkIDByHeight(lastAcceptedBlk.Height())
	switch err {
	case nil:
		// index is complete already.
		vm.latestPreForkHeight, err = vm.State.GetLatestPreForkHeight()
		return false, ids.Empty, err
	case database.ErrNotFound:
		// index needs repairing (and it's the first time we do this)
		return true, latestProBlkID, nil
	default:
		// Could not retrieve index from DB.
		// We got bigger problems than repairing the index
		return true, ids.Empty, err
	}
}

// if height index needs repairing, doRepair would do that. It
// iterates back via parents, checking and rebuilding height indexing
// heightIndexNeedsRepairing should be called asynchronously upon VM initialization.
func (vm *VM) doRepair(repairStartBlkID ids.ID) error {
	var (
		currentProBlkID   = repairStartBlkID
		currentInnerBlkID = ids.Empty

		startTime                 = time.Now()
		lastLogTime               = startTime
		indexedBlks               = 0
		pendingBytesApproximation = 0 // tracks of the size of uncommitted writes
	)

	for {
		currentAcceptedBlk, err := vm.getPostForkBlock(currentProBlkID)
		switch err {
		case nil:

		case database.ErrNotFound:
			// visited all proposerVM blocks. Let's record forkHeight
			firstWrappedInnerBlk, err := vm.ChainVM.GetBlock(currentInnerBlkID)
			if err != nil {
				return err
			}
			innerForkBlk, err := vm.ChainVM.GetBlock(firstWrappedInnerBlk.Parent())
			if err != nil {
				return err
			}
			vm.latestPreForkHeight = innerForkBlk.Height()
			if err := vm.State.SetLatestPreForkHeight(vm.latestPreForkHeight); err != nil {
				return err
			}

			// Delete checkpoint and finally commit
			if err := vm.State.DeleteRepairCheckpoint(); err != nil {
				return err
			}
			if err := vm.db.Commit(); err != nil {
				return err
			}
			vm.ctx.Log.Info("Block indexing by height completed: indexed %d blocks, duration %v, latest pre fork block height %d",
				indexedBlks, time.Since(startTime), vm.latestPreForkHeight)
			return nil

		default:
			return err
		}

		currentInnerBlkID = currentAcceptedBlk.getInnerBlk().ID()
		_, err = vm.State.GetBlkIDByHeight(currentAcceptedBlk.Height())
		switch err {
		case nil:
			// mapping already there; It must be the same for all ancestors and fork height too.
			// just load latestPreForkHeight
			vm.latestPreForkHeight, err = vm.State.GetLatestPreForkHeight()
			vm.ctx.Log.Info("Block indexing by height completed: indexed %d blocks, duration %v, latest pre fork block height %d",
				indexedBlks, time.Since(startTime), vm.latestPreForkHeight)
			return err

		case database.ErrNotFound:
			// Let's keep memory footprint under control by committing when a size threshold is reached
			// We commit before storing lastAcceptedBlk mapping so to use lastAcceptedBlk as nextBlkIDToResumeFrom
			if pendingBytesApproximation > commitSizeCap {
				if err := vm.State.SetRepairCheckpoint(currentProBlkID); err != nil {
					return err
				}
				if err := vm.db.Commit(); err != nil {
					return err
				}

				vm.ctx.Log.Info("Block indexing by height ongoing: indexed %d blocks", indexedBlks)
				vm.ctx.Log.Info("Block indexing by height ongoing: committed %d bytes, latest committed height %d",
					pendingBytesApproximation, currentAcceptedBlk.Height()+1)
				pendingBytesApproximation = 0
			}

			// mapping must have been introduced after snowman++ fork. Rebuild it.
			estimatedByteLen, err := vm.State.SetBlkIDByHeight(currentAcceptedBlk.Height(), currentProBlkID)
			if err != nil {
				return err
			}
			pendingBytesApproximation += estimatedByteLen

			// Periodically log progress
			indexedBlks++
			if time.Since(lastLogTime) > 15*time.Second {
				lastLogTime = time.Now()
				vm.ctx.Log.Info("Block indexing by height ongoing: indexed %d blocks", indexedBlks)
			}

			// keep checking the parent
			currentProBlkID = currentAcceptedBlk.Parent()

		default:
			return err
		}
	}
}

// Upon initialization, repairHeightIndex ensures the height -> proBlkID
// mapping is well formed. Starting from last accepted proposerVM block,
// it will go back to snowman++ activation fork or genesis.
// repairHeightIndex can take a non-trivial time to complete; hence we make sure
// the process has limited memory footprint, can be resumed from periodic checkpoints
// and asynchronously without stopping VM.
func (vm *VM) repairHeightIndex() error {
	doRepair, startBlkID, err := vm.shouldRepair()
	if !doRepair || err != nil {
		return err
	}

	go func() {
		vm.ctx.Log.AssertNoError(vm.doRepair(startBlkID))
	}()

	return nil
}
