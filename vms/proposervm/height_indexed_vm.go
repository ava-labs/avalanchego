// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/units"
)

const commitSizeCap = 1 * units.MiB

var (
	_ block.HeightIndexedChainVM = &VM{}

	errRepairNoPostForkBlocks = errors.New("no post fork block to repair")
)

func (vm *VM) HeightIndexingEnabled() bool {
	hVM, ok := vm.ChainVM.(block.HeightIndexedChainVM)
	if !ok {
		return false
	}

	if !hVM.HeightIndexingEnabled() {
		return false
	}

	// If height indexing is not complete, return we mark HeightIndexedChainVM as disabled.
	// even if vm.ChainVM is ready to serve blocks by height
	_, err := vm.State.GetRepairCheckpoint()
	switch err {
	case nil:
		return false
	case database.ErrNotFound:
		// Either indexing is complete or repairing it has not started yet.
		break
	default:
		return false
	}

	lastAcceptedBlkID, err := vm.LastAccepted()
	if err != nil {
		return false
	}
	lastAcceptedBlk, err := vm.GetBlock(lastAcceptedBlkID)
	if err != nil {
		return false
	}

	_, indexComplete := vm.State.GetBlkIDByHeight(lastAcceptedBlk.Height())
	return indexComplete == nil
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

// Upon initialization, repairInnerBlocksMapping ensures the height -> proBlkID
// mapping is well formed. Starting from last accepted proposerVM block,
// it will go back to snowman++ activation fork or genesis.
// repairInnerBlocksMapping can take a non-trivial time to complete; hence we make sure
// the process has limited memory footprint, can be resumed from periodic checkpoints
// and asynchronously without stopping VM.
func (vm *VM) repairInnerBlockMapping() error {
	if _, ok := vm.ChainVM.(block.HeightIndexedChainVM); !ok {
		// nothing to index if innerVM does not support height indexing
		return nil
	}

	var (
		latestProBlkID   ids.ID
		latestInnerBlkID = ids.Empty
		err              error

		startTime                 = time.Now()
		lastLogTime               = startTime
		indexedBlks               = 0
		pendingBytesApproximation = 0 // tracks of the size of uncommitted writes
	)

	// First the top proposerVM block to start with.
	latestProBlkID, err = vm.pickStartBlkIDToRepair()
	switch err {
	case nil:

	case errRepairNoPostForkBlocks:
		// no proposerVM blocks; empty chain or snowman++ not yet active
		if err := vm.repairForkHeight(latestInnerBlkID); err != nil {
			return err
		}
		if err := vm.checkpointAndCommit(ids.Empty); err != nil {
			return err
		}
		vm.ctx.Log.Info("Block indexing by height completed: indexed %d blocks, duration %v, latest pre fork block height %d",
			indexedBlks, time.Since(startTime), vm.latestPreForkHeight)
		return nil

	default:
		return err
	}

	// iterate back via parents, checking and rebuilding height indexing
	for {
		lastAcceptedBlk, err := vm.getPostForkBlock(latestProBlkID)
		switch err {
		case nil:

		case database.ErrNotFound:
			// visited all proposerVM blocks.
			if err := vm.repairForkHeight(latestInnerBlkID); err != nil {
				return err
			}
			if err := vm.checkpointAndCommit(ids.Empty); err != nil {
				return err
			}
			vm.ctx.Log.Info("Block indexing by height completed: indexed %d blocks, duration %v, latest pre fork block height %d",
				indexedBlks, time.Since(startTime), vm.latestPreForkHeight)
			return nil

		default:
			return err
		}

		latestInnerBlkID = lastAcceptedBlk.getInnerBlk().ID()
		_, err = vm.State.GetBlkIDByHeight(lastAcceptedBlk.Height())
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
				if err := vm.checkpointAndCommit(latestProBlkID); err != nil {
					return err
				}
				vm.ctx.Log.Info("Block indexing by height ongoing: indexed %d blocks", indexedBlks)
				vm.ctx.Log.Info("Block indexing by height ongoing: committed %d bytes, latest committed height %d",
					pendingBytesApproximation, lastAcceptedBlk.Height()+1)
				pendingBytesApproximation = 0
			}

			// mapping must have been introduced after snowman++ fork. Rebuild it.
			estimatedByteLen, err := vm.State.SetBlkIDByHeight(lastAcceptedBlk.Height(), latestProBlkID)
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
			latestProBlkID = lastAcceptedBlk.Parent()

		default:
			return err
		}
	}
}

func (vm *VM) pickStartBlkIDToRepair() (ids.ID, error) {
	// It's either the last accepted block or an intermediate block,
	// if the process was already started and interrupted.
	var (
		latestProBlkID ids.ID
		err            error
	)

	latestProBlkID, err = vm.State.GetRepairCheckpoint()
	switch err {
	case nil:
		vm.ctx.Log.Info("Block indexing by height starting: resuming from %v", latestProBlkID)
		return latestProBlkID, nil
	case database.ErrNotFound:
		// process was not interrupted. Try picking latest accepted proposerVM block
		break
	default:
		return ids.Empty, err
	}

	latestProBlkID, err = vm.State.GetLastAccepted() // this won't return preFork blks
	switch err {
	case nil:
		vm.ctx.Log.Info("Block indexing by height starting: starting from last accepted block %v", latestProBlkID)
		return latestProBlkID, nil
	case database.ErrNotFound:
		return ids.Empty, errRepairNoPostForkBlocks
	default:
		return ids.Empty, err
	}
}

func (vm *VM) repairForkHeight(latestInnerBlkID ids.ID) error {
	var (
		lastPreForkHeight uint64
		err               error
	)

	if latestInnerBlkID == ids.Empty {
		// not a single proposerVM block. Set fork height to current inner chain height
		latestInnerBlkID, err = vm.ChainVM.LastAccepted()
		if err != nil {
			return err
		}
		lastInnerBlk, err := vm.ChainVM.GetBlock(latestInnerBlkID)
		if err != nil {
			return err
		}
		lastPreForkHeight = lastInnerBlk.Height()
	} else {
		firstWrappedInnerBlk, err := vm.ChainVM.GetBlock(latestInnerBlkID)
		if err != nil {
			return err
		}
		candidateForkBlk, err := vm.ChainVM.GetBlock(firstWrappedInnerBlk.Parent())
		if err != nil {
			return err
		}
		lastPreForkHeight = candidateForkBlk.Height()
	}

	vm.latestPreForkHeight = lastPreForkHeight
	return vm.State.SetLatestPreForkHeight(vm.latestPreForkHeight)
}

func (vm *VM) checkpointAndCommit(checkpointBlkID ids.ID) error {
	// Empty nextBlkIDToResumeFrom signals rebuild process is done
	if checkpointBlkID == ids.Empty {
		if err := vm.State.DeleteRepairCheckpoint(); err != nil {
			return err
		}
	} else {
		if err := vm.State.SetRepairCheckpoint(checkpointBlkID); err != nil {
			return err
		}
	}

	return vm.db.Commit()
}
