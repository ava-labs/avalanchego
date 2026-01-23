// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

type migrator struct {
	// Databases
	evmDB      ethdb.Database
	headerDB   database.HeightIndex
	bodyDB     database.HeightIndex
	receiptsDB database.HeightIndex

	// Concurrency control
	mu     sync.Mutex // protects cancel and done
	cancel context.CancelFunc
	done   chan struct{}

	// Migration state
	completed atomic.Bool
	processed atomic.Uint64
	endHeight uint64

	logger logging.Logger
}

var migratorDBPrefix = []byte("migrator")

func (db *Database) initMigrator() error {
	mdb := prefixdb.New(migratorDBPrefix, db.metaDB)
	migrator, err := newMigrator(
		mdb,
		db.headerDB,
		db.bodyDB,
		db.receiptsDB,
		db.Database,
		db.logger,
	)
	if err != nil {
		return err
	}
	db.migrator = migrator
	return nil
}

// StartMigration begins the background migration of block data from the
// [ethdb.Database] to the height-indexed block databases.
//
// Returns an error if the databases are not initialized.
// No error if already running.
func (db *Database) StartMigration(ctx context.Context) error {
	if !db.heightDBsReady {
		return errNotInitialized
	}
	db.migrator.start(ctx)
	return nil
}

// targetBlockHeightKey stores the head height captured at first run for ETA.
var targetBlockHeightKey = []byte("migration_target_block_height")

func targetBlockHeight(db database.KeyValueReader) (uint64, bool, error) {
	has, err := db.Has(targetBlockHeightKey)
	if err != nil {
		return 0, false, err
	}
	if !has {
		return 0, false, nil
	}
	numBytes, err := db.Get(targetBlockHeightKey)
	if err != nil {
		return 0, false, err
	}
	if len(numBytes) != blockNumberSize {
		return 0, false, fmt.Errorf("invalid block number encoding length: %d", len(numBytes))
	}
	height := binary.BigEndian.Uint64(numBytes)
	return height, true, nil
}

func writeTargetBlockHeight(db database.KeyValueWriter, endHeight uint64) error {
	return db.Put(targetBlockHeightKey, encodeBlockNumber(endHeight))
}

func headBlockNumber(db ethdb.KeyValueReader) (uint64, bool) {
	hash := rawdb.ReadHeadHeaderHash(db)
	num := rawdb.ReadHeaderNumber(db, hash)
	if num == nil || *num == 0 {
		return 0, false
	}
	return *num, true
}

func isMigratableKey(db ethdb.Reader, key []byte) bool {
	if key[0] != evmBlockBodyPrefix {
		return false
	}
	num, hash, ok := parseBlockKey(key)
	if !ok {
		return false
	}

	// Skip genesis since all vms have it and to benefit from being able to have a
	// minimum height greater than 0 when state sync is enabled.
	if num == 0 {
		return false
	}

	canonHash := rawdb.ReadCanonicalHash(db, num)
	return canonHash == hash
}

func minBlockHeightToMigrate(db ethdb.Database) (uint64, bool, error) {
	iter := db.NewIterator([]byte{evmBlockBodyPrefix}, nil)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		if !isMigratableKey(db, key) {
			continue
		}
		num, _, ok := parseBlockKey(key)
		if !ok {
			return 0, false, errUnexpectedKey
		}
		return num, true, nil
	}
	return 0, false, iter.Error()
}

func newMigrator(
	db database.Database,
	headerDB database.HeightIndex,
	bodyDB database.HeightIndex,
	receiptsDB database.HeightIndex,
	evmDB ethdb.Database,
	logger logging.Logger,
) (*migrator, error) {
	m := &migrator{
		headerDB:   headerDB,
		bodyDB:     bodyDB,
		receiptsDB: receiptsDB,
		evmDB:      evmDB,
		logger:     logger,
	}

	_, ok, err := minBlockHeightToMigrate(evmDB)
	if err != nil {
		return nil, err
	}
	if !ok {
		m.completed.Store(true)
		m.logger.Info("No block data to migrate; migration already complete")
		return m, nil
	}

	// load saved end block height
	endHeight, ok, err := targetBlockHeight(db)
	if err != nil {
		return nil, err
	}
	if !ok {
		// load and save head block number as end block height
		if num, ok := headBlockNumber(evmDB); ok {
			endHeight = num
			if err := writeTargetBlockHeight(db, endHeight); err != nil {
				return nil, err
			}
			m.logger.Info(
				"Migration target height set",
				zap.Uint64("targetHeight", endHeight),
			)
		}
	}
	m.endHeight = endHeight

	return m, nil
}

func (m *migrator) isCompleted() bool {
	return m.completed.Load()
}

// stopTimeout is the maximum time to wait for migration to stop gracefully.
// 5 seconds allows cleanup operations to complete without blocking shutdown indefinitely.
const stopTimeout = 5 * time.Second

func (m *migrator) stop() {
	// Snapshot cancel/done under lock to avoid data race with endRun.
	// We must release the lock before waiting on done to prevent deadlock.
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.mu.Unlock()

	if cancel == nil {
		return // no active migration
	}

	cancel()
	select {
	case <-done:
		// worker finished cleanup
	case <-time.After(stopTimeout):
		m.logger.Warn("Migration shutdown timeout exceeded")
	}
}

func (m *migrator) beginRun(ctx context.Context) (context.Context, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		return nil, false // migration already running
	}
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})
	m.processed.Store(0)
	return ctx, true
}

func (m *migrator) endRun() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cancel = nil
	m.done = nil
}

// start begins the migration process in a background goroutine.
// Returns immediately if migration is already completed or running.
func (m *migrator) start(ctx context.Context) {
	if m.isCompleted() {
		return
	}
	ctx, ok := m.beginRun(ctx)
	if !ok {
		m.logger.Warn("Migration already running")
		return
	}

	go func() {
		defer func() {
			close(m.done)
			m.endRun()
		}()
		if err := m.run(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				m.logger.Error("Migration failed", zap.Error(err))
			}
		}
	}()
}

// waitMigratorDone waits until the current migration run completes.
// If timeout <= 0, it waits indefinitely.
// Returns true if completed, false on timeout.
func (m *migrator) waitMigratorDone(timeout time.Duration) bool {
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

func (m *migrator) migrateHeader(num uint64, hash common.Hash) error {
	header := rawdb.ReadHeader(m.evmDB, hash, num)
	if header == nil {
		return fmt.Errorf("header not found for block %d hash %s", num, hash)
	}
	hBytes, err := rlp.EncodeToBytes(header)
	if err != nil {
		return fmt.Errorf("failed to encode block header: %w", err)
	}
	if err := writeHashAndData(m.headerDB, num, hash, hBytes); err != nil {
		return fmt.Errorf("failed to write header to headerDB: %w", err)
	}
	return nil
}

func (m *migrator) migrateBody(num uint64, hash common.Hash, body []byte) error {
	if err := writeHashAndData(m.bodyDB, num, hash, body); err != nil {
		return fmt.Errorf("failed to write body to bodyDB: %w", err)
	}
	return nil
}

func (m *migrator) migrateReceipts(num uint64, hash common.Hash) error {
	receipts := rawdb.ReadReceiptsRLP(m.evmDB, hash, num)
	if receipts == nil {
		return nil
	}

	if err := writeHashAndData(m.receiptsDB, num, hash, receipts); err != nil {
		return fmt.Errorf("failed to write receipts to receiptsDB: %w", err)
	}
	return nil
}

func deleteBlock(db ethdb.KeyValueWriter, num uint64, hash common.Hash) error {
	// rawdb.DeleteHeader is not used to avoid deleting number/hash mappings.
	headerKey := blockHeaderKey(num, hash)
	if err := db.Delete(headerKey); err != nil {
		return fmt.Errorf("failed to delete header from evmDB: %w", err)
	}
	rawdb.DeleteBody(db, hash, num)
	rawdb.DeleteReceipts(db, hash, num)
	return nil
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
		logger.Error("Failed to compact data in range",
			zap.Uint64("startHeight", startNum),
			zap.Uint64("endHeight", endNum),
			zap.Error(err))
	}
}

func (m *migrator) compactBlockRange(startNum, endNum uint64) {
	start := time.Now()

	compactRange(m.evmDB, blockHeaderKey, startNum, endNum, m.logger)
	compactRange(m.evmDB, blockBodyKey, startNum, endNum, m.logger)
	compactRange(m.evmDB, receiptsKey, startNum, endNum, m.logger)

	m.logger.Info("Compaction of block range completed",
		zap.Uint64("startHeight", startNum),
		zap.Uint64("endHeight", endNum),
		zap.Duration("duration", time.Since(start)))
}

const (
	// logProgressInterval controls how often migration progress is logged.
	logProgressInterval = 30 * time.Second
	// compactionInterval is the number of blocks to process before compacting the database.
	compactionInterval = 250_000
)

func (m *migrator) run(ctx context.Context) error {
	m.logger.Info(
		"Block data migration started",
		zap.Uint64("targetHeight", m.endHeight),
	)

	var (
		// Progress tracking
		etaTarget  uint64 // target # of blocks to process
		etaTracker = timer.NewEtaTracker(10, 1)
		start      = time.Now()
		nextLog    = start.Add(logProgressInterval)

		// Batch to accumulate delete operations before writing
		batch       = m.evmDB.NewBatch()
		lastCompact uint64 // blocks processed at last compaction

		// Compaction tracking
		canCompact    bool
		startBlockNum uint64
		endBlockNum   uint64

		// Iterate over block bodies instead of headers since there are keys
		// under the header prefix that we are not migrating (e.g., hash mappings).
		iter = m.evmDB.NewIterator([]byte{evmBlockBodyPrefix}, nil)
	)

	defer func() {
		iter.Release()

		if batch.ValueSize() > 0 {
			if err := batch.Write(); err != nil {
				m.logger.Error("Failed to write final delete batch", zap.Error(err))
			}
		}

		// Compact final range if we processed any blocks after last interval compaction.
		if canCompact {
			m.compactBlockRange(startBlockNum, endBlockNum)
		}

		duration := time.Since(start)
		m.logger.Info(
			"Block data migration ended",
			zap.Uint64("targetHeight", m.endHeight),
			zap.Uint64("blocksProcessed", m.processed.Load()),
			zap.Uint64("lastProcessedHeight", endBlockNum),
			zap.Duration("duration", duration),
			zap.Bool("completed", m.isCompleted()),
		)
	}()

	// Iterate over all block bodies in ascending order by block number.
	for iter.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		key := iter.Key()
		if !isMigratableKey(m.evmDB, key) {
			continue
		}
		num, hash, ok := parseBlockKey(key)
		if !ok {
			return errUnexpectedKey
		}

		if etaTarget == 0 && m.endHeight > 0 && num < m.endHeight {
			etaTarget = m.endHeight - num
			etaTracker.AddSample(0, etaTarget, start)
		}

		// track the range of blocks for compaction
		if !canCompact {
			startBlockNum = num
			canCompact = true
		}
		endBlockNum = num

		if err := m.migrateHeader(num, hash); err != nil {
			return fmt.Errorf("failed to migrate header data: %w", err)
		}
		if err := m.migrateBody(num, hash, iter.Value()); err != nil {
			return fmt.Errorf("failed to migrate body data: %w", err)
		}
		if err := m.migrateReceipts(num, hash); err != nil {
			return fmt.Errorf("failed to migrate receipts data: %w", err)
		}
		if err := deleteBlock(batch, num, hash); err != nil {
			return fmt.Errorf("failed to add block deletes to batch: %w", err)
		}
		processed := m.processed.Add(1)

		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return fmt.Errorf("failed to write delete batch: %w", err)
			}
			batch.Reset()
		}

		// compact every compactionInterval blocks
		if canCompact && processed-lastCompact >= compactionInterval {
			// write any remaining deletes in batch before compaction
			if batch.ValueSize() > 0 {
				if err := batch.Write(); err != nil {
					return fmt.Errorf("failed to write delete batch before compaction: %w", err)
				}
				batch.Reset()
			}

			iter.Release()
			m.compactBlockRange(startBlockNum, endBlockNum)
			startKey := encodeBlockNumber(num + 1)
			newIter := m.evmDB.NewIterator([]byte{evmBlockBodyPrefix}, startKey)
			iter = newIter
			lastCompact = processed
			canCompact = false
		}

		// log progress every logProgressInterval
		if now := time.Now(); now.After(nextLog) {
			fields := []zap.Field{
				zap.Uint64("blocksProcessed", processed),
				zap.Uint64("lastProcessedHeight", num),
				zap.Duration("timeElapsed", time.Since(start)),
			}
			if etaTarget > 0 {
				eta, pct := etaTracker.AddSample(processed, etaTarget, now)
				if eta != nil {
					fields = append(fields,
						zap.Duration("eta", *eta),
						zap.String("progress", fmt.Sprintf("%.2f%%", pct)),
					)
				}
			}

			m.logger.Info("Block data migration progress", fields...)
			nextLog = now.Add(logProgressInterval)
		}
	}

	if iter.Error() != nil {
		return fmt.Errorf("failed to iterate over evmDB: %w", iter.Error())
	}

	m.completed.Store(true)
	return nil
}
