// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexes

import (
	"math"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
)

const commitSizeCap = 1 * units.MiB

var _ HeightIndexer = &heightIndexer{}

type HeightIndexer interface {
	block.HeightIndexedChainVM

	RepairHeightIndex() error

	// called for postFork blocks only!
	// TODO ABENEGIA: find a better name maybe
	UpdateHeightIndex(height uint64, blkID ids.ID) error
}

func NewHeightIndexer(srv BlockServer,
	innerHVM block.HeightIndexedChainVM,
	log logging.Logger,
	indexState state.HeightIndex,
	shutdownChan chan struct{},
	shutdownWg *sync.WaitGroup) HeightIndexer {
	return newHeightIndexer(srv, innerHVM, log, indexState, shutdownChan, shutdownWg)
}

func newHeightIndexer(srv BlockServer,
	innerHVM block.HeightIndexedChainVM,
	log logging.Logger,
	indexState state.HeightIndex,
	shutdownChan chan struct{},
	shutdownWg *sync.WaitGroup) *heightIndexer {
	res := &heightIndexer{
		server:       srv,
		innerHVM:     innerHVM,
		log:          log,
		shutdownChan: shutdownChan,
		shutdownWg:   shutdownWg,
		indexState:   indexState,
	}
	res.forkHeight.SetValue(uint64(math.MaxUint64))

	return res
}

type heightIndexer struct {
	server       BlockServer
	innerHVM     block.HeightIndexedChainVM
	log          logging.Logger
	shutdownChan chan struct{}
	shutdownWg   *sync.WaitGroup

	forkHeight utils.AtomicInterface // height of the first postFork block/option
	indexState state.HeightIndex
}

// Upon initialization, RepairHeightIndex ensures the height -> proBlkID
// height block index is well formed. Starting from last accepted proposerVM block,
// it will go back to snowman++ activation fork or genesis.
// RepairHeightIndex can take a non-trivial time to complete; hence we make sure
// the process has limited memory footprint, can be resumed from periodic checkpoints
// and asynchronously without stopping VM.
func (hi *heightIndexer) RepairHeightIndex() error {
	defer hi.shutdownWg.Done()
	needRepair, startBlkID, err := hi.shouldRepair()
	if err != nil {
		hi.log.Info("Block indexing by height starting: failed. Could not determine if index is complete, error %v", err)
		return err
	}

	if !needRepair {
		return nil
	}

	hi.log.Info("Block indexing by height starting: success. Retrieved checkpoint %v", startBlkID)
	return hi.doRepair(startBlkID)
}

// HeightIndexingEnabled implements HeightIndexedChainVM interface
func (hi *heightIndexer) IsHeightIndexComplete() bool {
	if hi.innerHVM == nil || !hi.innerHVM.IsHeightIndexComplete() {
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
	if hi.innerHVM == nil || !hi.innerHVM.IsHeightIndexComplete() {
		// innerVM does not support height index
		return ids.Empty, block.ErrHeightIndexedVMNotImplemented
	}

	// preFork blocks are indexed in innerVM only
	if height < hi.forkHeight.GetValue().(uint64) {
		return hi.innerHVM.GetBlockIDByHeight(height)
	}

	// postFork blocks are indexed in proposerVM
	return hi.indexState.GetBlockIDAtHeight(height)
}

func (hi *heightIndexer) UpdateHeightIndex(height uint64, blkID ids.ID) error {
	if hi.innerHVM == nil || !hi.innerHVM.IsHeightIndexComplete() {
		// nothing to index if innerVM does not support height indexing
		return nil
	}

	if hi.forkHeight.GetValue().(uint64) > height {
		hi.log.Info("Block indexing by height: new block. Moved fork height from %d to %d with block %v",
			hi.forkHeight.GetValue().(uint64), height, blkID)
		hi.forkHeight.SetValue(height)
		if err := hi.indexState.SetForkHeight(height); err != nil {
			hi.log.Info("Block indexing by height: new block. Failed storing new fork height %v", err)
			return err
		}
	}

	_, err := hi.indexState.SetBlockIDAtHeight(height, blkID)
	return err
}

// shouldRepair checks if height index is complete;
// if not, it returns the checkpoint from which repairing should start.
func (hi *heightIndexer) shouldRepair() (bool, ids.ID, error) {
	if hi.innerHVM == nil || !hi.innerHVM.IsHeightIndexComplete() {
		// no index, nothing to repair
		return false, ids.Empty, nil
	}

	switch checkpointID, err := hi.indexState.GetCheckpoint(); err {
	case nil:
		// checkpoint found, repair must be resumed
		return true, checkpointID, nil
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
		// snowman++ has not forked yet; height block index is ok.
		// forkHeight set at math.MaxUint64, aka +infinity
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

	_, err = hi.indexState.GetBlockIDAtHeight(lastAcceptedBlk.Height())
	switch err {
	case nil:
		// index is complete already.
		forkHeight, err := hi.indexState.GetForkHeight()
		if err != nil {
			return true, ids.Empty, err
		}
		hi.forkHeight.SetValue(forkHeight)
		return false, ids.Empty, nil
	case database.ErrNotFound:
		// index needs repairing and it's the first time we do this.
		// Mark the checkpoint so that, in case new blocks are accepted while
		// indexing is ongoing, and the process is terminated before first commit,
		// we do not miss rebuilding the full index.
		if err := hi.indexState.SetCheckpoint(latestProBlkID); err != nil {
			return true, ids.Empty, err
		}
		if err := hi.server.DBCommit(); err != nil {
			return true, ids.Empty, err
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

		start                     = time.Now()
		lastLogTime               = start
		indexedBlks               = 0
		pendingBytesApproximation = 0 // tracks of the size of uncommitted writes
	)

	for {
		currentAcceptedBlk, err := hi.server.GetWrappingBlk(currentProBlkID)
		switch err {
		case nil:
			select {
			case <-hi.shutdownChan:
				return hi.closeIndexer(currentAcceptedBlk)
			default:
				// go ahead with index repairing
			}

		case database.ErrNotFound:
			// visited all proposerVM blocks. Let's record forkHeight
			firstWrappedInnerBlk, err := hi.server.GetInnerBlk(currentInnerBlkID)
			if err != nil {
				return err
			}
			forkHeight := firstWrappedInnerBlk.Height()
			hi.forkHeight.SetValue(forkHeight)
			if err := hi.indexState.SetForkHeight(forkHeight); err != nil {
				return err
			}

			// Delete checkpoint and finally commit
			if err := hi.indexState.DeleteCheckpoint(); err != nil {
				return err
			}
			if err := hi.server.DBCommit(); err != nil {
				return err
			}
			hi.log.Info("Block indexing by height: completed. Indexed %d blocks, duration %v, fork height %d",
				indexedBlks, time.Since(start), hi.forkHeight.GetValue().(uint64))
			return nil

		default:
			return err
		}

		currentInnerBlkID = currentAcceptedBlk.GetInnerBlk().ID()
		_, err = hi.indexState.GetBlockIDAtHeight(currentAcceptedBlk.Height())
		switch err {
		case nil:
			// height block index already there; It must be the same for all ancestors and fork height too.
			// just load latestPreForkHeight
			forkHeight, err := hi.indexState.GetForkHeight()
			hi.forkHeight.SetValue(forkHeight)
			hi.log.Info("Block indexing by height: completed. Indexed %d blocks, duration %v, fork height %d",
				indexedBlks, time.Since(start), forkHeight)
			return err

		case database.ErrNotFound:
			// Let's keep memory footprint under control by committing when a size threshold is reached
			// We commit before storing lastAcceptedBlk height block index so to use lastAcceptedBlk as nextBlkIDToResumeFrom
			if pendingBytesApproximation > commitSizeCap {
				if err := hi.indexState.SetCheckpoint(currentProBlkID); err != nil {
					return err
				}
				if err := hi.server.DBCommit(); err != nil {
					return err
				}

				hi.log.Info("Block indexing by height: ongoing. Indexed %d blocks, latest committed height %d, committed %d bytes,  checkpoint %v",
					indexedBlks, currentAcceptedBlk.Height()+1, pendingBytesApproximation, currentProBlkID)
				pendingBytesApproximation = 0
			}

			// height block index must have been introduced after snowman++ fork. Rebuild it.
			estimatedByteLen, err := hi.indexState.SetBlockIDAtHeight(currentAcceptedBlk.Height(), currentProBlkID)
			if err != nil {
				return err
			}
			pendingBytesApproximation += estimatedByteLen

			// Periodically log progress
			indexedBlks++
			if time.Since(lastLogTime) > 15*time.Second {
				lastLogTime = time.Now()
				hi.log.Info("Block indexing by height: ongoing. Indexed %d blocks, latest indexed height %d",
					indexedBlks, currentAcceptedBlk.Height()+1)
			}

			// keep checking the parent
			currentProBlkID = currentAcceptedBlk.Parent()

		default:
			return err
		}
	}
}

func (hi *heightIndexer) closeIndexer(currentProBlk WrappingBlock) error {
	hi.log.Info("Block indexing by height shutdown: Started.")
	if err := hi.indexState.SetCheckpoint(currentProBlk.ID()); err != nil {
		hi.log.Info("Block indexing by height: shutdown. Failed setting checkpoint %v", err)
		return err
	}
	if err := hi.server.DBCommit(); err != nil {
		hi.log.Info("Block indexing by height shutdown: Failed committing DB %v", err)
		return err
	}
	hi.log.Info("Block indexing by height shutdown: done. Stored checkpoint %v at height %d",
		currentProBlk.ID(), currentProBlk.Height())
	return nil
}
