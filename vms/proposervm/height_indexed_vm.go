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

const (
	commitSizeCap = 10 * units.MiB
)

var _ block.HeightIndexedChainVM = &VM{}

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

// Upon initialization, repairInnerBlocksMapping ensures the height -> proBlkID
// mapping is well formed. Starting from last accepted proposerVM block,
// it will go back to snowman++ activation fork or genesis.
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

	latestProBlkID, err = vm.State.GetLastAccepted() // this won't return preFork blks
	switch err {
	case nil:
	case database.ErrNotFound:
		// no proposerVM blocks; maybe snowman++ not yet active
		goto checkFork
	default:
		return err
	}

	for {
		lastAcceptedBlk, err := vm.getPostForkBlock(latestProBlkID)
		switch err {
		case nil:
		case database.ErrNotFound:
			// visited all proposerVM blocks.
			goto checkFork
		default:
			return err
		}

		latestInnerBlkID = lastAcceptedBlk.getInnerBlk().ID()
		_, err = vm.State.GetBlkIDByHeight(lastAcceptedBlk.Height())
		switch err {
		case nil:
			// mapping already there; It must be the same for all ancestors too.
			// just update latestPreForkHeight
			vm.latestPreForkHeight, err = vm.State.GetLatestPreForkHeight()
			return err
		case database.ErrNotFound:
			// mapping must have been introduced after snowman++ fork. Rebuild it.
			estimatedByteLen, err := vm.State.SetBlkIDByHeight(lastAcceptedBlk.Height(), latestProBlkID)
			if err != nil {
				return err
			}

			// enforce soft maximal commit size
			pendingBytesApproximation += estimatedByteLen
			if pendingBytesApproximation > commitSizeCap {
				if err := vm.db.Commit(); err != nil {
					return err
				}
				vm.ctx.Log.Info("Block indexing by height ongoing: committed %d bytes, latest committed height %d",
					pendingBytesApproximation, lastAcceptedBlk.Height())
				pendingBytesApproximation = 0
			}

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

checkFork:
	var lastPreForkHeight uint64
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
	if err := vm.State.SetLatestPreForkHeight(vm.latestPreForkHeight); err != nil {
		return nil
	}

	vm.ctx.Log.Info("Block indexing by height completed: indexed %d blocks, duration %v, latest pre fork block height %d",
		indexedBlks, time.Since(startTime), vm.latestPreForkHeight)
	return vm.db.Commit()
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
