// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
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
	var (
		latestProBlkID   ids.ID
		latestInnerBlkID = ids.Empty
		err              error
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
			if err := vm.State.SetBlkIDByHeight(lastAcceptedBlk.Height(), latestProBlkID); err != nil {
				return err
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
	return vm.db.Commit()
}
