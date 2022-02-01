// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
)

// default number of heights to index before committing
const defaultCommitFrequency = 1024

var _ HeightIndexer = &heightIndexer{}

type HeightIndexer interface {
	// checks whether the index is fully repaired or not
	IsRepaired() bool

	// checks whether index rebuilding is needed and if so, performs it
	RepairHeightIndex() error
}

func NewHeightIndexer(
	srv BlockServer,
	log logging.Logger,
	indexState state.HeightIndex,
) HeightIndexer {
	return newHeightIndexer(srv, log, indexState)
}

func newHeightIndexer(
	srv BlockServer,
	log logging.Logger,
	indexState state.HeightIndex,
) *heightIndexer {
	return &heightIndexer{
		server:          srv,
		log:             log,
		indexState:      indexState,
		commitFrequency: defaultCommitFrequency,
	}
}

type heightIndexer struct {
	server BlockServer
	log    logging.Logger

	jobDone    utils.AtomicBool
	indexState state.HeightIndex

	commitFrequency int
}

func (hi *heightIndexer) IsRepaired() bool {
	return hi.jobDone.GetValue()
}

// RepairHeightIndex ensures the height -> proBlkID height block index is well formed.
// Starting from last accepted proposerVM block, it will go back to snowman++ activation fork
// or genesis. PreFork blocks will be handled by innerVM height index.
// RepairHeightIndex can take a non-trivial time to complete; hence we make sure
// the process has limited memory footprint, can be resumed from periodic checkpoints
// and works asynchronously without blocking the VM.
func (hi *heightIndexer) RepairHeightIndex() error {
	needRepair, startBlkID, err := hi.shouldRepair()
	if err != nil {
		return fmt.Errorf("could not determine if index should be repaired: %w", err)
	}
	if err := hi.flush(); err != nil {
		return fmt.Errorf("could not write height index updates: %w", err)
	}

	if !needRepair {
		forkHeight, err := hi.indexState.GetForkHeight()
		switch err {
		case nil:
			hi.log.Info("Block indexing by height: already complete. Fork height %d", forkHeight)
			return nil
		case database.ErrNotFound:
			hi.log.Info("Block indexing by height: already complete. Fork not reached yet.")
			return nil
		default:
			return err
		}
	}
	if err := hi.doRepair(startBlkID); err != nil {
		return fmt.Errorf("could not repair height index: %w", err)
	}
	if err := hi.flush(); err != nil {
		return fmt.Errorf("could not write final height index update: %w", err)
	}
	return nil
}

// shouldRepair checks if height index is complete;
// if not, it returns the checkpoint from which repairing should start.
// Note: batch commit is deferred to shouldRepair caller
func (hi *heightIndexer) shouldRepair() (bool, ids.ID, error) {
	checkpointID, err := hi.indexState.GetCheckpoint()
	if err != database.ErrNotFound {
		// if checkpoint is found, re-indexing can start.
		// if unexpected error is returned, there nothing we can really do here.
		return true, checkpointID, err
	}

	// no checkpoint. Either index is complete or repair was never attempted.
	// index is complete iff lastAcceptedBlock is indexed
	latestProBlkID, err := hi.server.LastAcceptedWrappingBlkID()
	switch err {
	case nil:
		break

	case database.ErrNotFound:
		// snowman++ has not forked yet; height block index is ok.
		hi.jobDone.SetValue(true)
		hi.log.Info("Block indexing by height starting: Snowman++ fork not reached yet. No need to rebuild index.")
		return false, ids.Empty, nil

	default:
		return true, ids.Empty, err
	}

	lastAcceptedBlk, err := hi.server.GetWrappingBlk(latestProBlkID)
	if err != nil {
		// Could not retrieve last accepted block.
		// We got bigger problems than repairing the index
		return true, ids.Empty, err
	}

	_, err = hi.indexState.GetBlockIDAtHeight(lastAcceptedBlk.Height())
	switch err {
	case nil:
		// index is complete already.
		hi.jobDone.SetValue(true)
		hi.log.Info("Block indexing by height starting: Index already complete, nothing to do.")
		return false, ids.Empty, nil

	case database.ErrNotFound:
		// Index needs repairing. Mark the checkpoint so that,
		// in case new blocks are accepted while indexing is ongoing,
		// and the process is terminated before first commit,
		// we do not miss rebuilding the full index.
		if err := hi.indexState.SetCheckpoint(latestProBlkID); err != nil {
			return true, ids.Empty, err
		}

		// it will commit on exit
		hi.log.Info("Block indexing by height starting: index incomplete. Rebuilding from %v", latestProBlkID)
		return true, latestProBlkID, nil

	default:
		return true, ids.Empty, err
	}
}

// if height index needs repairing, doRepair would do that. It
// iterates back via parents, checking and rebuilding height indexing.
// Note: batch commit is deferred to doRepair caller
func (hi *heightIndexer) doRepair(currentProBlkID ids.ID) error {
	var (
		start           = time.Now()
		lastLogTime     = start
		indexedBlks     int
		lastIndexedBlks int
		previousHeight  uint64
	)
	for {
		currentAcceptedBlk, err := hi.server.GetWrappingBlk(currentProBlkID)
		if err == database.ErrNotFound {
			// We have visited all the proposerVM blocks. Because we previously
			// verified that we needed to perform a repair, we know that this
			// will not happen on the first iteration. This guarantees that
			// [previousHeight] will be correctly initialized.
			if err := hi.indexState.SetForkHeight(previousHeight); err != nil {
				return err
			}
			if err := hi.indexState.DeleteCheckpoint(); err != nil {
				return err
			}
			hi.jobDone.SetValue(true)

			// it will commit on exit
			hi.log.Info(
				"Block indexing by height: completed. Indexed %d blocks, duration %v, fork height %d",
				indexedBlks,
				time.Since(start),
				previousHeight,
			)
			return nil
		}
		if err != nil {
			return err
		}

		currentHeight := currentAcceptedBlk.Height()
		_, err = hi.indexState.GetBlockIDAtHeight(currentHeight)
		if err == nil {
			// index completed. This may happen when node shuts down while
			// accepting a new block.

			if err := hi.indexState.DeleteCheckpoint(); err != nil {
				return err
			}
			hi.jobDone.SetValue(true)

			// it will commit on exit
			hi.log.Info(
				"Block indexing by height: repaired. Indexed %d blocks, duration %v",
				indexedBlks,
				time.Since(start),
			)
			return nil
		}
		if err != database.ErrNotFound {
			return err
		}

		// Keep memory footprint under control by committing when a size threshold is reached
		if indexedBlks-lastIndexedBlks > hi.commitFrequency {
			// Note: checkpoint must be the lowest block in the batch. This ensures that
			// checkpoint is the highest un-indexed block from which process would restart.
			if err := hi.indexState.SetCheckpoint(currentProBlkID); err != nil {
				return err
			}

			if err := hi.flush(); err != nil {
				return err
			}

			hi.log.Info(
				"Block indexing by height: ongoing. Indexed %d blocks, latest committed height %d",
				indexedBlks,
				currentHeight,
			)
			lastIndexedBlks = indexedBlks
		}

		// Rebuild height block index.
		if err := hi.indexState.SetBlockIDAtHeight(currentHeight, currentProBlkID); err != nil {
			return err
		}

		// Periodically log progress
		indexedBlks++
		now := time.Now()
		if now.Sub(lastLogTime) > 15*time.Second {
			lastLogTime = now
			hi.log.Info(
				"Block indexing by height: ongoing. Indexed %d blocks, latest indexed height %d",
				indexedBlks,
				currentHeight,
			)
		}

		// keep checking the parent
		currentProBlkID = currentAcceptedBlk.Parent()
		previousHeight = currentHeight
	}
}

// flush writes the commits to the underlying DB
func (hi *heightIndexer) flush() error {
	if err := hi.indexState.Commit(); err != nil {
		return err
	}
	return hi.server.Commit()
}
