// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
)

const (
	batchWritePeriod      = 1
	iteratorReleasePeriod = 1024
	logPeriod             = 5 * time.Second
)

type Parser interface {
	ParseBlock(context.Context, []byte) (snowman.Block, error)
}

func GetMissingBlockIDs(
	ctx context.Context,
	db database.KeyValueReader,
	parser Parser,
	tree *Tree,
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

		blkBytes, err := GetBlock(db, i.LowerBound)
		if err != nil {
			return nil, err
		}

		blk, err := parser.ParseBlock(ctx, blkBytes)
		if err != nil {
			return nil, err
		}

		parentID := blk.Parent()
		missingBlocks.Add(parentID)
	}
	return missingBlocks, nil
}

// Add the block to the tree and return if the parent block should be fetched.
func Add(
	db database.KeyValueWriterDeleter,
	tree *Tree,
	lastAcceptedHeight uint64,
	blk snowman.Block,
) (bool, error) {
	var (
		height            = blk.Height()
		lastHeightToFetch = lastAcceptedHeight + 1
	)
	if height < lastHeightToFetch || tree.Contains(height) {
		return false, nil
	}

	blkBytes := blk.Bytes()
	if err := PutBlock(db, height, blkBytes); err != nil {
		return false, err
	}

	if err := tree.Add(db, height); err != nil {
		return false, err
	}

	return height != lastHeightToFetch && !tree.Contains(height-1), nil
}

func Execute(
	ctx context.Context,
	log logging.Logger,
	db database.Database,
	parser Parser,
	tree *Tree,
	lastAcceptedHeight uint64,
) error {
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

		iterator                      = db.NewIteratorWithPrefix(blockPrefix)
		processedSinceIteratorRelease uint

		startTime            = time.Now()
		timeOfNextLog        = startTime.Add(logPeriod)
		totalNumberToProcess = tree.Len()
	)
	defer func() {
		iterator.Release()
	}()

	log.Info("executing blocks",
		zap.Uint64("numToExecute", totalNumberToProcess),
	)

	for iterator.Next() {
		blkBytes := iterator.Value()
		blk, err := parser.ParseBlock(ctx, blkBytes)
		if err != nil {
			return err
		}

		height := blk.Height()
		if err := DeleteBlock(batch, height); err != nil {
			return err
		}

		if err := tree.Remove(batch, height); err != nil {
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
			iterator = db.NewIteratorWithPrefix(blockPrefix)
		}

		now := time.Now()
		if now.After(timeOfNextLog) {
			numProcessed := totalNumberToProcess - tree.Len()
			eta := timer.EstimateETA(startTime, numProcessed, totalNumberToProcess)

			log.Info("executing blocks",
				zap.Duration("eta", eta),
				zap.Uint64("numExecuted", numProcessed),
				zap.Uint64("numToExecute", totalNumberToProcess),
			)
			timeOfNextLog = now.Add(logPeriod)
		}

		if height <= lastAcceptedHeight {
			continue
		}

		if err := blk.Verify(ctx); err != nil {
			return err
		}
		if err := blk.Accept(ctx); err != nil {
			return err
		}
	}
	if err := writeBatch(); err != nil {
		return err
	}
	if err := iterator.Error(); err != nil {
		return err
	}

	log.Info("executed blocks",
		zap.Uint64("numExecuted", totalNumberToProcess),
		zap.Duration("duration", time.Since(startTime)),
	)
	return nil
}
