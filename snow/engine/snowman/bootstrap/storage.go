// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap/interval"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
)

const (
	batchWritePeriod      = 64
	iteratorReleasePeriod = 1024
	logPeriod             = 5 * time.Second
	minBlocksToCompact    = 5000
)

// getMissingBlockIDs returns the ID of the blocks that should be fetched to
// attempt to make a single continuous range from
// (lastAcceptedHeight, highestTrackedHeight].
//
// For example, if the tree currently contains heights [1, 4, 6, 7] and the
// lastAcceptedHeight is 2, this function will return the IDs corresponding to
// blocks [3, 5].
func getMissingBlockIDs(
	ctx context.Context,
	db database.KeyValueReader,
	nonVerifyingParser block.Parser,
	tree *interval.Tree,
	lastAcceptedHeight uint64,
) (set.Set[ids.ID], error) {
	var (
		missingBlocks     set.Set[ids.ID]
		intervals         = tree.Flatten()
		lastHeightToFetch = lastAcceptedHeight + 1
	)
	for _, i := range intervals {
		if i.LowerBound <= lastHeightToFetch {
			continue
		}

		blkBytes, err := interval.GetBlock(db, i.LowerBound)
		if err != nil {
			return nil, err
		}

		blk, err := nonVerifyingParser.ParseBlock(ctx, blkBytes)
		if err != nil {
			return nil, err
		}

		parentID := blk.Parent()
		missingBlocks.Add(parentID)
	}
	return missingBlocks, nil
}

// process a series of consecutive blocks starting at [blk].
//
//   - blk is a block that is assumed to have been marked as acceptable by the
//     bootstrapping engine.
//   - ancestors is a set of blocks that can be used to lookup blocks.
//
// If [blk]'s height is <= the last accepted height, then it will be removed
// from the missingIDs set.
//
// Returns a newly discovered blockID that should be fetched.
func process(
	db database.KeyValueWriterDeleter,
	tree *interval.Tree,
	missingBlockIDs set.Set[ids.ID],
	lastAcceptedHeight uint64,
	blk snowman.Block,
	ancestors map[ids.ID]snowman.Block,
) (ids.ID, bool, error) {
	for {
		// It's possible that missingBlockIDs contain values contained inside of
		// ancestors. So, it's important to remove IDs from the set for each
		// iteration, not just the first block's ID.
		blkID := blk.ID()
		missingBlockIDs.Remove(blkID)

		height := blk.Height()
		blkBytes := blk.Bytes()
		wantsParent, err := interval.Add(
			db,
			tree,
			lastAcceptedHeight,
			height,
			blkBytes,
		)
		if err != nil || !wantsParent {
			return ids.Empty, false, err
		}

		// If the parent was provided in the ancestors set, we can immediately
		// process it.
		parentID := blk.Parent()
		parent, ok := ancestors[parentID]
		if !ok {
			return parentID, true, nil
		}

		blk = parent
	}
}

// execute all the blocks tracked by the tree. If a block is in the tree but is
// already accepted based on the lastAcceptedHeight, it will be removed from the
// tree but not executed.
//
// execute assumes that getMissingBlockIDs would return an empty set.
//
// TODO: Replace usage of haltable with context cancellation.
func execute(
	ctx context.Context,
	shouldHalt func() bool,
	log logging.Func,
	db database.Database,
	nonVerifyingParser block.Parser,
	tree *interval.Tree,
	lastAcceptedHeight uint64,
) error {
	totalNumberToProcess := tree.Len()
	if totalNumberToProcess >= minBlocksToCompact {
		log("compacting database before executing blocks...")
		if err := db.Compact(nil, nil); err != nil {
			// Not a fatal error, log and move on.
			log("failed to compact bootstrap database before executing blocks",
				zap.Error(err),
			)
		}
	}

	var (
		batch                    = db.NewBatch()
		processedSinceBatchWrite uint
		writeBatch               = func() error {
			if processedSinceBatchWrite == 0 {
				return nil
			}
			processedSinceBatchWrite = 0

			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
			return nil
		}

		iterator                      = interval.GetBlockIterator(db)
		processedSinceIteratorRelease uint

		startTime     = time.Now()
		timeOfNextLog = startTime.Add(logPeriod)
		etaTracker    = timer.NewEtaTracker(10, 1.2)
	)
	defer func() {
		iterator.Release()

		var (
			numProcessed = totalNumberToProcess - tree.Len()
			halted       = shouldHalt()
		)
		if numProcessed >= minBlocksToCompact && !halted {
			log("compacting database after executing blocks...")
			if err := db.Compact(nil, nil); err != nil {
				// Not a fatal error, log and move on.
				log("failed to compact bootstrap database after executing blocks",
					zap.Error(err),
				)
			}
		}

		log("executed blocks",
			zap.Uint64("numExecuted", numProcessed),
			zap.Uint64("numToExecute", totalNumberToProcess),
			zap.Bool("halted", halted),
			zap.Duration("duration", time.Since(startTime)),
		)
	}()

	log("executing blocks",
		zap.Uint64("numToExecute", totalNumberToProcess),
	)

	// Add the first sample to the EtaTracker to establish an accurate baseline
	etaTracker.AddSample(0, totalNumberToProcess, startTime)

	for !shouldHalt() && iterator.Next() {
		blkBytes := iterator.Value()
		blk, err := nonVerifyingParser.ParseBlock(ctx, blkBytes)
		if err != nil {
			return err
		}

		height := blk.Height()
		if err := interval.Remove(batch, tree, height); err != nil {
			return err
		}

		// Periodically write the batch to disk to avoid memory pressure.
		processedSinceBatchWrite++
		if processedSinceBatchWrite >= batchWritePeriod {
			if err := writeBatch(); err != nil {
				return err
			}
		}

		// Periodically release and re-grab the database iterator to avoid
		// keeping a reference to an old database revision.
		processedSinceIteratorRelease++
		if processedSinceIteratorRelease >= iteratorReleasePeriod {
			if err := iterator.Error(); err != nil {
				return err
			}

			// The batch must be written here to avoid re-processing a block.
			if err := writeBatch(); err != nil {
				return err
			}

			processedSinceIteratorRelease = 0
			iterator.Release()
			// We specify the starting key of the iterator so that the
			// underlying database doesn't need to scan over the, potentially
			// not yet compacted, blocks we just deleted.
			iterator = interval.GetBlockIteratorWithStart(db, height+1)
		}

		if now := time.Now(); now.After(timeOfNextLog) {
			numProcessed := totalNumberToProcess - tree.Len()

			// Use the tracked previous progress for accurate ETA calculation
			currentProgress := numProcessed

			etaPtr, progressPercentage := etaTracker.AddSample(currentProgress, totalNumberToProcess, now)
			// Only log if we have a valid ETA estimate
			if etaPtr != nil {
				log("executing blocks",
					zap.Uint64("numExecuted", numProcessed),
					zap.Uint64("numToExecute", totalNumberToProcess),
					zap.Duration("eta", *etaPtr),
					zap.Float64("pctComplete", progressPercentage),
				)
			}

			timeOfNextLog = now.Add(logPeriod)
		}

		if height <= lastAcceptedHeight {
			continue
		}

		if err := blk.Verify(ctx); err != nil {
			return fmt.Errorf("failed to verify block %s (height=%d, parentID=%s) in bootstrapping: %w",
				blk.ID(),
				height,
				blk.Parent(),
				err,
			)
		}
		if err := blk.Accept(ctx); err != nil {
			return fmt.Errorf("failed to accept block %s (height=%d, parentID=%s) in bootstrapping: %w",
				blk.ID(),
				height,
				blk.Parent(),
				err,
			)
		}
	}
	if err := writeBatch(); err != nil {
		return err
	}
	return iterator.Error()
}
