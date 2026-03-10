// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package migrate

import (
	"bytes"
	"fmt"
	"slices"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// chainIDPrefixLen is the length of the chain ID prefix (32 bytes)
	chainIDPrefixLen = 32

	// migrateBatchFlushSize is the byte threshold at which a write batch is
	// flushed to the target database during migration (~16 MB, matching
	// LevelDB's default write-buffer size).
	migrateBatchFlushSize = 16 * 1024 * 1024
)

// PerChainMigrator handles migration from monolithic database to per-chain databases.
type PerChainMigrator struct {
	sourceDB database.Database
	log      logging.Logger

	// chainDBs maps chain IDs to their target databases
	chainDBs map[ids.ID]database.Database

	// Statistics
	keysProcessed int
	keysCopied    int
	keysSkipped   int
}

// NewPerChainMigrator creates a new migrator for per-chain database separation.
func NewPerChainMigrator(sourceDB database.Database, log logging.Logger) *PerChainMigrator {
	return &PerChainMigrator{
		sourceDB: sourceDB,
		log:      log,
		chainDBs: make(map[ids.ID]database.Database),
	}
}

// RegisterChainDB registers a target database for a specific chain.
// Keys with this chain's prefix will be migrated to this database.
// Note: P-chain uses ids.Empty as its chain ID, which is valid.
func (m *PerChainMigrator) RegisterChainDB(chainID ids.ID, targetDB database.Database) error {
	if targetDB == nil {
		return fmt.Errorf("target database cannot be nil")
	}

	if _, exists := m.chainDBs[chainID]; exists {
		return fmt.Errorf("database already registered for chain %s", chainID)
	}

	m.chainDBs[chainID] = targetDB
	m.log.Info("registered target database for chain migration",
		zap.Stringer("chainID", chainID),
	)
	return nil
}

// Migrate performs the migration from monolithic to per-chain databases.
//
// Process:
//  1. Iterates through all keys in source database
//  2. Extracts chain ID from first 32 bytes of key
//  3. Copies key/value to appropriate chain database (stripping chain ID prefix)
//  4. Returns statistics about the migration
//
// NOTE: This does NOT delete keys from source database (non-destructive migration).
// The source database should be backed up and deleted manually after verification.
func (m *PerChainMigrator) Migrate() error {
	if len(m.chainDBs) == 0 {
		return fmt.Errorf("no target databases registered")
	}

	// Reset statistics for this migration run
	m.keysProcessed = 0
	m.keysCopied = 0
	m.keysSkipped = 0

	m.log.Info("starting per-chain database migration",
		zap.Int("targetChains", len(m.chainDBs)),
	)

	// One write batch per target chain DB for efficient bulk writes.
	batches := make(map[ids.ID]database.Batch, len(m.chainDBs))
	for chainID, db := range m.chainDBs {
		batches[chainID] = db.NewBatch()
	}
	flushBatch := func(chainID ids.ID) error {
		b := batches[chainID]
		if err := b.Write(); err != nil {
			return fmt.Errorf("failed to flush batch for chain %s: %w", chainID, err)
		}
		b.Reset()
		return nil
	}
	// On error paths, flush any remaining (unflushed) batch data best-effort.
	// The success path uses the explicit final flush loop below and sets this flag.
	var allFlushed bool
	defer func() {
		if allFlushed {
			return
		}
		for chainID := range batches {
			_ = flushBatch(chainID)
		}
	}()

	// Create iterator for entire source database
	iterator := m.sourceDB.NewIterator()
	defer iterator.Release()

	for iterator.Next() {
		m.keysProcessed++

		// Log progress every 10000 keys
		if m.keysProcessed%10000 == 0 {
			m.log.Info("migration progress",
				zap.Int("keysProcessed", m.keysProcessed),
				zap.Int("keysCopied", m.keysCopied),
				zap.Int("keysSkipped", m.keysSkipped),
			)
		}

		// Check key length before copying — short keys are skipped immediately.
		// iterator.Key() and iterator.Value() are only valid until the next Next() call.
		rawKey := iterator.Key()
		if len(rawKey) < chainIDPrefixLen {
			m.keysSkipped++
			m.log.Debug("skipping key without chain ID prefix",
				zap.Int("keyLength", len(rawKey)),
			)
			continue
		}

		// Extract chain ID from first 32 bytes
		chainID, err := ids.ToID(rawKey[:chainIDPrefixLen])
		if err != nil {
			m.keysSkipped++
			m.log.Debug("skipping key with invalid chain ID prefix",
				zap.Binary("prefix", rawKey[:chainIDPrefixLen]),
				zap.Error(err),
			)
			continue
		}

		// Check if we have a target database for this chain
		if _, exists := m.chainDBs[chainID]; !exists {
			m.keysSkipped++
			if m.keysSkipped%10000 == 0 {
				m.log.Debug("skipping key for unregistered chain",
					zap.Stringer("chainID", chainID),
				)
			}
			continue
		}

		// Now that we know this key will be written, copy key and value.
		keyWithoutPrefix := slices.Clone(rawKey[chainIDPrefixLen:])
		value := slices.Clone(iterator.Value())

		batch := batches[chainID]
		if err := batch.Put(keyWithoutPrefix, value); err != nil {
			return fmt.Errorf("failed to stage key for chain %s: %w", chainID, err)
		}
		m.keysCopied++

		// Flush batch when it reaches the size threshold
		if batch.Size() >= migrateBatchFlushSize {
			if err := flushBatch(chainID); err != nil {
				return err
			}
		}
	}

	if err := iterator.Error(); err != nil {
		return fmt.Errorf("iterator error during migration: %w", err)
	}

	// Flush all remaining batches
	for chainID := range batches {
		if err := flushBatch(chainID); err != nil {
			return err
		}
	}
	allFlushed = true

	m.log.Info("per-chain database migration completed",
		zap.Int("keysProcessed", m.keysProcessed),
		zap.Int("keysCopied", m.keysCopied),
		zap.Int("keysSkipped", m.keysSkipped),
	)

	return nil
}

// VerifyMigration checks that all migrated keys are present in target databases.
//
// This is a read-only verification step that should be run after Migrate()
// to ensure data integrity before deleting the source database.
func (m *PerChainMigrator) VerifyMigration() error {
	m.log.Info("verifying per-chain database migration")

	verified := 0
	errCount := 0

	iterator := m.sourceDB.NewIterator()
	defer iterator.Release()

	for iterator.Next() {
		rawKey := iterator.Key()

		// Skip keys without chain ID prefix
		if len(rawKey) < chainIDPrefixLen {
			continue
		}

		chainID, err := ids.ToID(rawKey[:chainIDPrefixLen])
		if err != nil {
			continue
		}

		targetDB, exists := m.chainDBs[chainID]
		if !exists {
			continue
		}

		// Copy key since we need it past the next iterator.Next() call.
		// Value is only needed for comparison; copy only if Get succeeds.
		keyWithoutPrefix := slices.Clone(rawKey[chainIDPrefixLen:])
		targetValue, err := targetDB.Get(keyWithoutPrefix)
		if err != nil {
			m.log.Error("verification failed: key missing in target database",
				zap.Stringer("chainID", chainID),
				zap.Binary("key", keyWithoutPrefix[:min(32, len(keyWithoutPrefix))]),
				zap.Error(err),
			)
			errCount++
			continue
		}

		// Compare directly against the iterator's value buffer — no copy needed here.
		if !bytes.Equal(iterator.Value(), targetValue) {
			m.log.Error("verification failed: value mismatch",
				zap.Stringer("chainID", chainID),
				zap.Binary("key", keyWithoutPrefix[:min(32, len(keyWithoutPrefix))]),
			)
			errCount++
			continue
		}

		verified++

		if verified%10000 == 0 {
			m.log.Info("verification progress",
				zap.Int("verified", verified),
				zap.Int("errors", errCount),
			)
		}
	}

	if err := iterator.Error(); err != nil {
		return fmt.Errorf("iterator error during verification: %w", err)
	}

	m.log.Info("migration verification completed",
		zap.Int("verified", verified),
		zap.Int("errors", errCount),
	)

	if errCount > 0 {
		return fmt.Errorf("verification failed with %d errors", errCount)
	}

	return nil
}

// GetStatistics returns migration statistics.
func (m *PerChainMigrator) GetStatistics() (keysProcessed, keysCopied, keysSkipped int) {
	return m.keysProcessed, m.keysCopied, m.keysSkipped
}
