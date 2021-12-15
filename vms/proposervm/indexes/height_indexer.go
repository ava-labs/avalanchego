// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexes

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
)

const commitSizeCap = 1 * units.MiB

var _ HeightIndexer = &heightIndexer{}

type HeightIndexer interface {
	block.HeightIndexedChainVM

	RepairHeightIndex()
	UpdateHeightIndex(height uint64, blkID ids.ID) error
	UpdateLatestPreForkBlockHeight(height uint64) error
}

func NewHeightIndexer(srv BlockServer,
	innerHVM block.HeightIndexedChainVM,
	log logging.Logger,
	indexState state.HeightIndex) HeightIndexer {
	return &heightIndexer{
		server:     srv,
		innerHVM:   innerHVM,
		log:        log,
		indexState: indexState,
	}
}

type heightIndexer struct {
	server   BlockServer
	innerHVM block.HeightIndexedChainVM
	log      logging.Logger

	latestPreForkHeight uint64
	indexState          state.HeightIndex
}

// Upon initialization, RepairHeightIndex ensures the height -> proBlkID
// height block index is well formed. Starting from last accepted proposerVM block,
// it will go back to snowman++ activation fork or genesis.
// RepairHeightIndex can take a non-trivial time to complete; hence we make sure
// the process has limited memory footprint, can be resumed from periodic checkpoints
// and asynchronously without stopping VM.
func (hi *heightIndexer) RepairHeightIndex() {
	doRepair, startBlkID, err := hi.shouldRepair()
	hi.log.AssertNoError(err)
	if !doRepair {
		return
	}

	hi.log.AssertNoError(hi.doRepair(startBlkID))
}

// HeightIndexingEnabled implements HeightIndexedChainVM interface
func (hi *heightIndexer) HeightIndexingEnabled() bool {
	if hi.innerHVM == nil || !hi.innerHVM.HeightIndexingEnabled() {
		// innerVM does not support height index
		return false
	}

	// If height indexing is not complete, we mark HeightIndexedChainVM as disabled,
	// even if vm.ChainVM is ready to serve blocks by height
	doRepair, _, err := hi.shouldRepair()
	if doRepair || err != nil {
		return false
	}

	return true
}

// GetBlockIDByHeight implements HeightIndexedChainVM interface
func (hi *heightIndexer) GetBlockIDByHeight(height uint64) (ids.ID, error) {
	if hi.innerHVM == nil || !hi.innerHVM.HeightIndexingEnabled() {
		// innerVM does not support height index
		return ids.Empty, block.ErrHeightIndexedVMNotImplemented
	}

	// preFork blocks are indexed in innerVM only
	if height <= hi.latestPreForkHeight {
		return hi.innerHVM.GetBlockIDByHeight(height)
	}

	// postFork blocks are indexed in proposerVM
	return hi.indexState.GetBlkIDByHeight(height)
}

func (hi *heightIndexer) UpdateHeightIndex(height uint64, blkID ids.ID) error {
	if hi.innerHVM == nil || !hi.innerHVM.HeightIndexingEnabled() {
		// nothing to index if innerVM does not support height indexing
		return nil
	}

	_, err := hi.indexState.SetBlkIDByHeight(height, blkID)
	hi.log.Debug("Block indexing by height: added post fork block at height %d", height)
	return err
}

func (hi *heightIndexer) UpdateLatestPreForkBlockHeight(height uint64) error {
	if hi.innerHVM == nil || !hi.innerHVM.HeightIndexingEnabled() {
		// nothing to index if innerVM does not support height indexing
		return nil
	}

	if height <= hi.latestPreForkHeight {
		return nil
	}

	hi.latestPreForkHeight = height
	hi.log.Debug("Block indexing by height: added pre fork block at height %d", height)
	return hi.indexState.SetLatestPreForkHeight(height)
}

// shouldRepair checks if height index is complete;
// if not, it returns highest un-indexed block ID from which repairing should start.
// shouldRepair should be called synchronously upon VM initialization.
func (hi *heightIndexer) shouldRepair() (bool, ids.ID, error) {
	if hi.innerHVM == nil || !hi.innerHVM.HeightIndexingEnabled() {
		// no index, nothing to repair
		return false, ids.Empty, nil
	}

	repairStartBlkID, err := hi.indexState.GetRepairCheckpoint()
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
	latestProBlkID, err := hi.server.LastAcceptedWrappingBlkID()
	switch err {
	case nil:
		break
	case database.ErrNotFound:
		// snowman++ has not forked yet. Height block index is ok;
		// just check latestPreForkHeight is duly set.
		latestInnerBlkID, err := hi.server.LastAcceptedInnerBlkID()
		if err != nil {
			return true, ids.Empty, err
		}
		lastInnerBlk, err := hi.server.GetInnerBlk(latestInnerBlkID)
		if err != nil {
			return true, ids.Empty, err
		}
		hi.latestPreForkHeight = lastInnerBlk.Height()
		if err := hi.indexState.SetLatestPreForkHeight(hi.latestPreForkHeight); err != nil {
			return true, ids.Empty, err
		}
		return false, ids.Empty, nil
	default:
		return true, ids.Empty, err
	}
	lastAcceptedBlk, err := hi.server.GetWrappingBlk(latestProBlkID)
	if err != nil {
		// Could not retrieve block for LastAccepted Block.
		// We got bigger problems than repairing the index
		return true, ids.Empty, err
	}

	_, err = hi.indexState.GetBlkIDByHeight(lastAcceptedBlk.Height())
	switch err {
	case nil:
		// index is complete already.
		hi.latestPreForkHeight, err = hi.indexState.GetLatestPreForkHeight()
		return false, ids.Empty, err
	case database.ErrNotFound:
		// index needs repairing and it's the first time we do this.
		// Mark the checkpoint so that, in case new blocks are accepted while
		// indexing is ongoing, and the process is terminated before first commit,
		// we do not miss rebuilding the full index
		if err := hi.indexState.SetRepairCheckpoint(latestProBlkID); err != nil {
			return false, ids.Empty, err
		}
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
func (hi *heightIndexer) doRepair(repairStartBlkID ids.ID) error {
	var (
		currentProBlkID   = repairStartBlkID
		currentInnerBlkID = ids.Empty

		startTime                 = time.Now()
		lastLogTime               = startTime
		indexedBlks               = 0
		pendingBytesApproximation = 0 // tracks of the size of uncommitted writes
	)

	for {
		currentAcceptedBlk, err := hi.server.GetWrappingBlk(currentProBlkID)
		switch err {
		case nil:

		case database.ErrNotFound:
			// visited all proposerVM blocks. Let's record forkHeight
			firstWrappedInnerBlk, err := hi.server.GetInnerBlk(currentInnerBlkID)
			if err != nil {
				return err
			}
			innerForkBlk, err := hi.server.GetInnerBlk(firstWrappedInnerBlk.Parent())
			if err != nil {
				return err
			}
			hi.latestPreForkHeight = innerForkBlk.Height()
			if err := hi.indexState.SetLatestPreForkHeight(hi.latestPreForkHeight); err != nil {
				return err
			}

			// Delete checkpoint and finally commit
			if err := hi.indexState.DeleteRepairCheckpoint(); err != nil {
				return err
			}
			if err := hi.server.DBCommit(); err != nil {
				return err
			}
			hi.log.Info("Block indexing by height completed: indexed %d blocks, duration %v, latest pre fork block height %d",
				indexedBlks, time.Since(startTime), hi.latestPreForkHeight)
			return nil

		default:
			return err
		}

		currentInnerBlkID = currentAcceptedBlk.GetInnerBlk().ID()
		_, err = hi.indexState.GetBlkIDByHeight(currentAcceptedBlk.Height())
		switch err {
		case nil:
			// height block index already there; It must be the same for all ancestors and fork height too.
			// just load latestPreForkHeight
			hi.latestPreForkHeight, err = hi.indexState.GetLatestPreForkHeight()
			hi.log.Info("Block indexing by height completed: indexed %d blocks, duration %v, latest pre fork block height %d",
				indexedBlks, time.Since(startTime), hi.latestPreForkHeight)
			return err

		case database.ErrNotFound:
			// Let's keep memory footprint under control by committing when a size threshold is reached
			// We commit before storing lastAcceptedBlk height block index so to use lastAcceptedBlk as nextBlkIDToResumeFrom
			if pendingBytesApproximation > commitSizeCap {
				if err := hi.indexState.SetRepairCheckpoint(currentProBlkID); err != nil {
					return err
				}
				if err := hi.server.DBCommit(); err != nil {
					return err
				}

				hi.log.Info("Block indexing by height ongoing: indexed %d blocks", indexedBlks)
				hi.log.Info("Block indexing by height ongoing: committed %d bytes, latest committed height %d",
					pendingBytesApproximation, currentAcceptedBlk.Height()+1)
				pendingBytesApproximation = 0
			}

			// height block index must have been introduced after snowman++ fork. Rebuild it.
			estimatedByteLen, err := hi.indexState.SetBlkIDByHeight(currentAcceptedBlk.Height(), currentProBlkID)
			if err != nil {
				return err
			}
			pendingBytesApproximation += estimatedByteLen

			// Periodically log progress
			indexedBlks++
			if time.Since(lastLogTime) > 15*time.Second {
				lastLogTime = time.Now()
				hi.log.Info("Block indexing by height ongoing: indexed %d blocks", indexedBlks)
			}

			// keep checking the parent
			currentProBlkID = currentAcceptedBlk.Parent()

		default:
			return err
		}
	}
}
