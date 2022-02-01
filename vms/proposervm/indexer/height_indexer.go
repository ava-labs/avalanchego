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
// Starting from the checkpoint, it will go back to snowman++ activation fork
// or genesis. PreFork blocks will be handled by innerVM height index.
// RepairHeightIndex can take a non-trivial time to complete; hence we make sure
// the process has limited memory footprint, can be resumed from periodic checkpoints
// and works asynchronously without blocking the VM.
func (hi *heightIndexer) RepairHeightIndex() error {
	startBlkID, err := hi.indexState.GetCheckpoint()
	if err == database.ErrNotFound {
		hi.jobDone.SetValue(true)
		return nil // nothing to do
	}
	if err != nil {
		return err
	}

	if err := hi.doRepair(startBlkID); err != nil {
		return fmt.Errorf("could not repair height index: %w", err)
	}
	if err := hi.flush(); err != nil {
		return fmt.Errorf("could not write final height index update: %w", err)
	}
	return nil
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
