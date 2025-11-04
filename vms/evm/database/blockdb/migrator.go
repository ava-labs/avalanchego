// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

const (
	logProgressInterval = 30 * time.Second
	compactionInterval  = 250_000
	stopTimeout         = 5 * time.Second
	etaSampleInterval   = 100
)

var (
	targetBlockNumKey = []byte("migration_target_block_number")
)

type migrator struct {
	stateDB    database.Database
	kvDB       ethdb.Database
	headerDB   database.HeightIndex
	bodyDB     database.HeightIndex
	receiptsDB database.HeightIndex

	mu     sync.Mutex // protects cancel and done
	cancel context.CancelFunc
	done   chan struct{}

	completed atomic.Bool
	processed atomic.Uint64
	endHeight uint64

	logger logging.Logger
}

func newMigrator(
	stateDB database.Database,
	headerDB database.HeightIndex,
	bodyDB database.HeightIndex,
	receiptsDB database.HeightIndex,
	kvDB ethdb.Database,
	logger logging.Logger,
) (*migrator, error) {
	m := &migrator{
		headerDB:   headerDB,
		bodyDB:     bodyDB,
		receiptsDB: receiptsDB,
		stateDB:    stateDB,
		kvDB:       kvDB,
		logger:     logger,
	}

	// Check if there's anything to migrate
	if _, ok := minBlockHeightToMigrate(kvDB); !ok {
		m.completed.Store(true)
		m.logger.Info("no block data to migrate; migration already complete")
		return m, nil
	}

	// load end block height
	endHeight, ok, err := getEndBlockHeight(stateDB)
	if err != nil {
		return nil, err
	}
	if !ok {
		// load and save head block number as end block height
		if headBlockNumber, ok := getHeadBlockNumber(kvDB); ok {
			endHeight = headBlockNumber
			if err := writeTargetBlockHeight(stateDB, endHeight); err != nil {
				return nil, err
			}
			m.logger.Info("migration target height set",
				zap.Uint64("targetHeight", endHeight))
		}
	}
	m.endHeight = endHeight

	return m, nil
}

func (m *migrator) isCompleted() bool {
	return m.completed.Load()
}

func (m *migrator) stop() {
	// Snapshot cancel/done to avoid TOCTOU race with endRun()
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.mu.Unlock()

	if cancel == nil {
		return // No active migration
	}

	// Signal worker to stop, then wait for cleanup
	cancel()
	if done != nil {
		select {
		case <-done:
			// Worker finished cleanup
		case <-time.After(stopTimeout):
			m.logger.Warn("migration shutdown timeout exceeded")
		}
	}
}

// start begins the migration process in a background goroutine.
// Returns immediately if migration is already completed or running.
func (m *migrator) start() {
	if m.isCompleted() {
		return
	}
	ctx, ok := m.beginRun()
	if !ok {
		return
	}

	go func() {
		// Close done channel first to unblock waiters, then clear references
		defer close(m.done)
		defer m.endRun()
		if err := m.run(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				m.logger.Error("migration failed", zap.Error(err))
			}
		}
	}()
}

func (m *migrator) beginRun() (context.Context, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	return ctx, true
}

func (m *migrator) endRun() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cancel = nil
	m.done = nil
}

func (m *migrator) run(ctx context.Context) error {
	var (
		etaTarget  uint64
		etaTracker = timer.NewEtaTracker(10, 1)
		start      = time.Now()
		nextLog    = start.Add(logProgressInterval)

		batch             = m.kvDB.NewBatch()
		lastCompactionCnt uint64

		canCompact    bool
		startBlockNum uint64
		endBlockNum   uint64

		// Iterate over block bodies instead of headers since there are keys
		// under the header prefix that we are not migrating.
		iter = m.kvDB.NewIterator(kvDBBlockBodyPrefix, nil)
	)

	defer func() {
		iter.Release()

		if err := batch.Write(); err != nil {
			m.logger.Error("failed to write final delete batch", zap.Error(err))
		}

		// Compact final range if we processed any blocks after last interval compaction
		if canCompact {
			m.compactBlockRange(startBlockNum, endBlockNum)
		}

		processingTime := time.Since(start)
		m.logger.Info("block data migration ended",
			zap.Uint64("blocksProcessed", m.processed.Load()),
			zap.Duration("totalProcessingTime", processingTime))
	}()

	m.logger.Info("block data migration started")

	// iterate over all block bodies in ascending order by block number
	for iter.Next() {
		// check if migration is stopped on every key iteration
		select {
		case <-ctx.Done():
			m.logger.Info(
				"block data migration stopped",
				zap.Uint64("blocksProcessed", m.processed.Load()),
			)
			return ctx.Err()
		default:
		}

		key := iter.Key()
		if !shouldMigrateKey(m.kvDB, key) {
			continue
		}

		blockNum, hash, err := blockNumberAndHashFromKey(key)
		if err != nil {
			return err
		}

		if etaTarget == 0 && m.endHeight > 0 && blockNum < m.endHeight {
			etaTarget = m.endHeight - blockNum
			etaTracker.AddSample(0, etaTarget, start)
		}

		// Track the range of blocks for compaction
		if !canCompact {
			startBlockNum = blockNum
			canCompact = true
		}
		endBlockNum = blockNum

		// Migrate block data (header + body)
		if err := m.migrateBlock(blockNum, hash, iter.Value()); err != nil {
			return fmt.Errorf("failed to migrate block data: %w", err)
		}

		// Migrate receipts
		if err := m.migrateReceipts(blockNum, hash); err != nil {
			return fmt.Errorf("failed to migrate receipt data: %w", err)
		}

		// Add deletes to batch
		if err := deleteBlock(batch, blockNum, hash); err != nil {
			return fmt.Errorf("failed to add block deletes to batch: %w", err)
		}
		processed := m.processed.Add(1)

		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return fmt.Errorf("failed to write delete batch: %w", err)
			}
			batch.Reset()
		}

		// Compact every compactionInterval blocks
		if processed-lastCompactionCnt >= compactionInterval {
			// Write any remaining deletes in batch before compaction
			if batch.ValueSize() > 0 {
				if err := batch.Write(); err != nil {
					return fmt.Errorf("failed to write delete batch before compaction: %w", err)
				}
				batch.Reset()
			}

			// Release iterator before compaction
			iter.Release()

			if canCompact {
				m.compactBlockRange(startBlockNum, endBlockNum)
			}

			iterStart := encodeBlockNumber(blockNum + 1)
			iter = m.kvDB.NewIterator(kvDBBlockBodyPrefix, iterStart)
			lastCompactionCnt = processed
			canCompact = false
		}

		// Log progress every logProgressInterval
		if now := time.Now(); now.After(nextLog) {
			logFields := []zap.Field{
				zap.Uint64("blocksProcessed", processed),
				zap.Uint64("lastProcessedHeight", blockNum),
				zap.Duration("timeElapsed", time.Since(start)),
			}
			if etaTarget > 0 {
				eta, pct := etaTracker.AddSample(processed, etaTarget, now)
				if eta != nil {
					logFields = slices.Concat(logFields, []zap.Field{
						zap.Duration("eta", *eta),
						zap.String("progress", fmt.Sprintf("%.2f%%", pct)),
					})
				}
			}

			m.logger.Info("block data migration progress", logFields...)
			nextLog = now.Add(logProgressInterval)
		}
	}

	if iter.Error() != nil {
		return fmt.Errorf("failed to iterate over kvDB: %w", iter.Error())
	}

	m.completed.Store(true)
	return nil
}

func (m *migrator) compactBlockRange(startNum, endNum uint64) {
	startTime := time.Now()

	compactRange(m.kvDB, blockHeaderKey, startNum, endNum, m.logger)
	compactRange(m.kvDB, blockBodyKey, startNum, endNum, m.logger)
	compactRange(m.kvDB, receiptsKey, startNum, endNum, m.logger)

	m.logger.Info("compaction of block range completed",
		zap.Uint64("startHeight", startNum),
		zap.Uint64("endHeight", endNum),
		zap.Duration("duration", time.Since(startTime)))
}

func (m *migrator) migrateBlock(num uint64, hash common.Hash, body []byte) error {
	header := rawdb.ReadHeader(m.kvDB, hash, num)
	if header == nil {
		return fmt.Errorf("header not found for block %d hash %s", num, hash)
	}
	headerBytes, err := rlp.EncodeToBytes(header)
	if err != nil {
		return fmt.Errorf("failed to encode block header: %w", err)
	}
	if err := writeHashAndData(m.headerDB, num, hash, headerBytes); err != nil {
		return fmt.Errorf("failed to write header to headersdb: %w", err)
	}
	if err := writeHashAndData(m.bodyDB, num, hash, body); err != nil {
		return fmt.Errorf("failed to write body to bodiesdb: %w", err)
	}
	return nil
}

func (m *migrator) migrateReceipts(num uint64, hash common.Hash) error {
	receipts := rawdb.ReadReceiptsRLP(m.kvDB, hash, num)
	if receipts == nil {
		// No receipts for this block, skip
		return nil
	}

	if err := writeHashAndData(m.receiptsDB, num, hash, receipts); err != nil {
		return fmt.Errorf("failed to write receipts to receiptsDB: %w", err)
	}
	return nil
}

// waitDone waits until the current migration run completes.
// If timeout <= 0, it waits indefinitely.
// Returns true if completed, false on timeout.
func (m *migrator) waitDone(timeout time.Duration) bool {
	// Snapshot done to avoid race with goroutine cleanup
	m.mu.Lock()
	done := m.done
	m.mu.Unlock()

	if done == nil {
		return true
	}
	if timeout <= 0 {
		<-done
		return true
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	}
}

func deleteBlock(db ethdb.KeyValueWriter, num uint64, hash common.Hash) error {
	headerKey := blockHeaderKey(num, hash)
	if err := db.Delete(headerKey); err != nil {
		return fmt.Errorf("failed to delete header from kvDB: %w", err)
	}
	rawdb.DeleteBody(db, hash, num)
	rawdb.DeleteReceipts(db, hash, num)
	return nil
}

func getEndBlockHeight(db database.KeyValueReader) (uint64, bool, error) {
	blockNumberBytes, err := db.Get(targetBlockNumKey)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(blockNumberBytes) != blockNumberSize {
		return 0, false, fmt.Errorf("invalid block number encoding length: %d", len(blockNumberBytes))
	}
	endHeight := binary.BigEndian.Uint64(blockNumberBytes)
	return endHeight, true, nil
}

func getHeadBlockNumber(db ethdb.KeyValueReader) (uint64, bool) {
	headHash := rawdb.ReadHeadHeaderHash(db)
	headBlockNumber := rawdb.ReadHeaderNumber(db, headHash)
	if headBlockNumber == nil || *headBlockNumber == 0 {
		return 0, false
	}
	return *headBlockNumber, true
}

func writeTargetBlockHeight(db database.KeyValueWriter, endHeight uint64) error {
	return db.Put(targetBlockNumKey, encodeBlockNumber(endHeight))
}

func shouldMigrateKey(db ethdb.Reader, key []byte) bool {
	if !isBodyKey(key) {
		return false
	}
	blockNum, hash, err := blockNumberAndHashFromKey(key)
	if err != nil {
		return false
	}

	// Skip genesis blocks to avoid complicating state-sync min-height handling.
	if blockNum == 0 {
		return false
	}

	canonicalHash := rawdb.ReadCanonicalHash(db, blockNum)
	return canonicalHash == hash
}

func blockHeaderKey(num uint64, hash common.Hash) []byte {
	return slices.Concat(kvDBHeaderPrefix, encodeBlockNumber(num), hash.Bytes())
}

func blockBodyKey(num uint64, hash common.Hash) []byte {
	return slices.Concat(kvDBBlockBodyPrefix, encodeBlockNumber(num), hash.Bytes())
}

func receiptsKey(num uint64, hash common.Hash) []byte {
	return slices.Concat(kvDBReceiptsPrefix, encodeBlockNumber(num), hash.Bytes())
}

func minBlockHeightToMigrate(db ethdb.Database) (uint64, bool) {
	iter := db.NewIterator(kvDBBlockBodyPrefix, nil)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		if !shouldMigrateKey(db, key) {
			continue
		}
		blockNum, _, err := blockNumberAndHashFromKey(key)
		if err != nil {
			continue
		}
		return blockNum, true
	}
	return 0, false
}

func compactRange(
	db ethdb.Compacter,
	keyFunc func(uint64, common.Hash) []byte,
	startNum, endNum uint64,
	logger logging.Logger,
) {
	startKey := keyFunc(startNum, common.Hash{})
	endKey := keyFunc(endNum+1, common.Hash{})
	if err := db.Compact(startKey, endKey); err != nil {
		logger.Error("failed to compact data in range",
			zap.Uint64("startHeight", startNum),
			zap.Uint64("endHeight", endNum),
			zap.Error(err))
	}
}
