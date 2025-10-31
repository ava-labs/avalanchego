// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reexecute

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

type vmExecutor struct {
	log              logging.Logger
	vm               block.ChainVM
	metrics          *consensusMetrics
	executionTimeout time.Duration
	startBlock       uint64
	endBlock         uint64
	etaTracker       *timer.EtaTracker
}

func newVMExecutor(
	log logging.Logger,
	vm block.ChainVM,
	registry prometheus.Registerer,
	executionTimeout time.Duration,
	startBlock uint64,
	endBlock uint64,
) (*vmExecutor, error) {
	metrics, err := newConsensusMetrics(registry)
	if err != nil {
		return nil, fmt.Errorf("failed to create consensus metrics: %w", err)
	}

	return &vmExecutor{
		log:              log,
		vm:               vm,
		metrics:          metrics,
		executionTimeout: executionTimeout,
		startBlock:       startBlock,
		endBlock:         endBlock,
		// ETA tracker uses a 10-sample moving window to smooth rate estimates,
		// and a 1.2 slowdown factor to slightly pad ETA early in the run,
		// tapering to 1.0 as progress approaches 100%.
		etaTracker: timer.NewEtaTracker(10, 1.2),
	}, nil
}

func (e *vmExecutor) execute(ctx context.Context, blockBytes []byte) error {
	blk, err := e.vm.ParseBlock(ctx, blockBytes)
	if err != nil {
		return fmt.Errorf("failed to parse block: %w", err)
	}
	if err := blk.Verify(ctx); err != nil {
		return fmt.Errorf("failed to verify block %s at height %d: %w", blk.ID(), blk.Height(), err)
	}

	if err := blk.Accept(ctx); err != nil {
		return fmt.Errorf("failed to accept block %s at height %d: %w", blk.ID(), blk.Height(), err)
	}
	e.metrics.lastAcceptedHeight.Set(float64(blk.Height()))

	return nil
}

func (e *vmExecutor) executeSequence(ctx context.Context, blkChan <-chan blockResult) error {
	blkID, err := e.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}
	blk, err := e.vm.GetBlock(ctx, blkID)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block by blkID %s: %w", blkID, err)
	}

	start := time.Now()
	e.log.Info("last accepted block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", blk.Height()),
	)

	// Initialize ETA tracking with a baseline sample at 0 progress
	totalWork := e.endBlock - e.startBlock
	e.etaTracker.AddSample(0, totalWork, start)

	if e.executionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.executionTimeout)
		defer cancel()
	}

	for blkResult := range blkChan {
		if blkResult.err != nil {
			return blkResult.err
		}

		if blkResult.height%1000 == 0 {
			completed := blkResult.height - e.startBlock
			etaPtr, progressPercentage := e.etaTracker.AddSample(completed, totalWork, time.Now())
			if etaPtr != nil {
				e.log.Info("executing block",
					zap.Uint64("height", blkResult.height),
					zap.Float64("progress_pct", progressPercentage),
					zap.Duration("eta", *etaPtr),
				)
			} else {
				e.log.Info("executing block",
					zap.Uint64("height", blkResult.height),
					zap.Float64("progress_pct", progressPercentage),
				)
			}
		}
		if err := e.execute(ctx, blkResult.blockBytes); err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			e.log.Info("exiting early due to context timeout",
				zap.Duration("elapsed", time.Since(start)),
				zap.Duration("execution-timeout", e.executionTimeout),
				zap.Error(ctx.Err()),
			)
			return nil
		}
	}
	e.log.Info("finished executing sequence")

	return nil
}

type blockResult struct {
	blockBytes []byte
	height     uint64
	err        error
}

func createBlockChanFromLevelDB(tb testing.TB, sourceDir string, startBlock, endBlock uint64, chanSize int) (<-chan blockResult, error) {
	r := require.New(tb)
	ch := make(chan blockResult, chanSize)

	db, err := leveldb.New(sourceDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		return nil, fmt.Errorf("failed to create leveldb database from %q: %w", sourceDir, err)
	}
	tb.Cleanup(func() {
		r.NoError(db.Close())
	})

	go func() {
		defer close(ch)

		iter := db.NewIteratorWithStart(blockKey(startBlock))
		defer iter.Release()

		currentHeight := startBlock

		for iter.Next() {
			key := iter.Key()
			if len(key) != database.Uint64Size {
				ch <- blockResult{
					blockBytes: nil,
					err:        fmt.Errorf("expected key length %d while looking for block at height %d, got %d", database.Uint64Size, currentHeight, len(key)),
				}
				return
			}
			height := binary.BigEndian.Uint64(key)
			if height != currentHeight {
				ch <- blockResult{
					blockBytes: nil,
					err:        fmt.Errorf("expected next height %d, got %d", currentHeight, height),
				}
				return
			}
			ch <- blockResult{
				blockBytes: iter.Value(),
				height:     height,
			}
			currentHeight++
			if currentHeight > endBlock {
				break
			}
		}
		if iter.Error() != nil {
			ch <- blockResult{
				blockBytes: nil,
				err:        fmt.Errorf("failed to iterate over blocks at height %d: %w", currentHeight, iter.Error()),
			}
			return
		}
	}()

	return ch, nil
}

func blockKey(height uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, height)
}
