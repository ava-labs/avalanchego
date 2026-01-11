// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package migrate

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// chainIDPrefixLen is the length of the chain ID prefix (32 bytes)
	chainIDPrefixLen = 32
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

	m.log.Info("starting per-chain database migration",
		zap.Int("targetChains", len(m.chainDBs)),
	)

	// Create iterator for entire source database
	iterator := m.sourceDB.NewIterator()
	defer iterator.Release()

	// Process each key
	for iterator.Next() {
		// IMPORTANT: iterator.Key() and iterator.Value() return slices that may be reused
		// on the next iteration. We must copy them before using.
		key := copyBytes(iterator.Key())
		value := copyBytes(iterator.Value())

		m.keysProcessed++

		// Log progress every 10000 keys
		if m.keysProcessed%10000 == 0 {
			m.log.Info("migration progress",
				zap.Int("keysProcessed", m.keysProcessed),
				zap.Int("keysCopied", m.keysCopied),
				zap.Int("keysSkipped", m.keysSkipped),
			)
		}

		// Check if key has chain ID prefix
		if len(key) < chainIDPrefixLen {
			m.keysSkipped++
			m.log.Debug("skipping key without chain ID prefix",
				zap.Int("keyLength", len(key)),
			)
			continue
		}

		// Extract chain ID from first 32 bytes
		chainIDBytes := key[:chainIDPrefixLen]
		chainID, err := ids.ToID(chainIDBytes)
		if err != nil {
			m.keysSkipped++
			m.log.Debug("skipping key with invalid chain ID prefix",
				zap.Binary("prefix", chainIDBytes),
				zap.Error(err),
			)
			continue
		}

		// Check if we have a target database for this chain
		targetDB, exists := m.chainDBs[chainID]
		if !exists {
			m.keysSkipped++
			// Don't log every skipped key - too verbose
			if m.keysSkipped%10000 == 0 {
				m.log.Debug("skipping key for unregistered chain",
					zap.Stringer("chainID", chainID),
				)
			}
			continue
		}

		// Strip chain ID prefix from key
		keyWithoutPrefix := key[chainIDPrefixLen:]

		// Copy to target database
		if err := targetDB.Put(keyWithoutPrefix, value); err != nil {
			return fmt.Errorf("failed to write key to chain %s database: %w", chainID, err)
		}

		m.keysCopied++
	}

	// Check for iterator errors
	if err := iterator.Error(); err != nil {
		return fmt.Errorf("iterator error during migration: %w", err)
	}

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
	errors := 0

	// Create iterator for entire source database
	iterator := m.sourceDB.NewIterator()
	defer iterator.Release()

	// Verify each key
	for iterator.Next() {
		// IMPORTANT: iterator.Key() and iterator.Value() return slices that may be reused
		key := copyBytes(iterator.Key())
		value := copyBytes(iterator.Value())

		// Skip keys without chain ID prefix
		if len(key) < chainIDPrefixLen {
			continue
		}

		// Extract chain ID
		chainIDBytes := key[:chainIDPrefixLen]
		chainID, err := ids.ToID(chainIDBytes)
		if err != nil {
			continue
		}

		// Check if we migrated this chain
		targetDB, exists := m.chainDBs[chainID]
		if !exists {
			continue // Not migrated, skip
		}

		// Verify key exists in target database
		keyWithoutPrefix := key[chainIDPrefixLen:]
		targetValue, err := targetDB.Get(keyWithoutPrefix)
		if err != nil {
			m.log.Error("verification failed: key missing in target database",
				zap.Stringer("chainID", chainID),
				zap.Binary("key", keyWithoutPrefix[:min(32, len(keyWithoutPrefix))]),
				zap.Error(err),
			)
			errors++
			continue
		}

		// Verify value matches
		if !bytesEqual(value, targetValue) {
			m.log.Error("verification failed: value mismatch",
				zap.Stringer("chainID", chainID),
				zap.Binary("key", keyWithoutPrefix[:min(32, len(keyWithoutPrefix))]),
			)
			errors++
			continue
		}

		verified++

		// Log progress
		if verified%10000 == 0 {
			m.log.Info("verification progress",
				zap.Int("verified", verified),
				zap.Int("errors", errors),
			)
		}
	}

	if err := iterator.Error(); err != nil {
		return fmt.Errorf("iterator error during verification: %w", err)
	}

	m.log.Info("migration verification completed",
		zap.Int("verified", verified),
		zap.Int("errors", errors),
	)

	if errors > 0 {
		return fmt.Errorf("verification failed with %d errors", errors)
	}

	return nil
}

// GetStatistics returns migration statistics.
func (m *PerChainMigrator) GetStatistics() (keysProcessed, keysCopied, keysSkipped int) {
	return m.keysProcessed, m.keysCopied, m.keysSkipped
}

// Helper functions

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyBytes creates a copy of a byte slice.
// This is necessary because database iterators may reuse the underlying buffer.
func copyBytes(src []byte) []byte {
	if src == nil {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}
