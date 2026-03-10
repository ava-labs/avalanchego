// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

// MigrationConfig defines configuration for database migration
type MigrationConfig struct {
	// BatchSize is the number of key-value pairs to migrate in each batch
	// Larger batches are faster but use more memory
	// Recommended: 10000 for most cases
	BatchSize int

	// ProgressInterval is how often to log progress updates
	// Set to 0 to disable progress logging
	ProgressInterval time.Duration

	// VerifyAfterMigration enables data verification after migration completes
	// This doubles migration time but ensures data integrity
	VerifyAfterMigration bool

	// ContinueOnError determines whether to continue migration if individual
	// key-value pairs fail to migrate. If false, migration stops on first error.
	ContinueOnError bool
}

// DefaultMigrationConfig returns sensible defaults for migration
func DefaultMigrationConfig() MigrationConfig {
	return MigrationConfig{
		BatchSize:            10000,
		ProgressInterval:     30 * time.Second,
		VerifyAfterMigration: true,
		ContinueOnError:      false,
	}
}

// MigrationStats tracks migration progress and performance
type MigrationStats struct {
	TotalKeys      uint64
	TotalBytes     uint64
	ErrorCount     uint64
	StartTime      time.Time
	EndTime        time.Time
	LastUpdateTime time.Time
}

// KeysPerSecond calculates migration throughput
func (s *MigrationStats) KeysPerSecond() float64 {
	elapsed := time.Since(s.StartTime).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(s.TotalKeys) / elapsed
}

// BytesPerSecond calculates migration throughput in bytes
func (s *MigrationStats) BytesPerSecond() float64 {
	elapsed := time.Since(s.StartTime).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(s.TotalBytes) / elapsed
}

// MigrateDatabase migrates all data from source to destination database
//
// This function:
// 1. Iterates over all key-value pairs in source database
// 2. Writes them to destination database in batches
// 3. Optionally verifies data after migration
// 4. Reports progress and statistics
//
// Parameters:
//   - ctx: Context for cancellation
//   - source: Source database (typically LevelDB)
//   - dest: Destination database (Firewood)
//   - config: Migration configuration
//   - log: Logger for progress reporting
//
// Returns migration statistics and any error encountered.
func MigrateDatabase(
	ctx context.Context,
	source database.Database,
	dest database.Database,
	config MigrationConfig,
	log logging.Logger,
) (*MigrationStats, error) {
	stats := &MigrationStats{
		StartTime:      time.Now(),
		LastUpdateTime: time.Now(),
	}

	log.Info("starting database migration",
		zap.Int("batchSize", config.BatchSize),
		zap.Bool("verify", config.VerifyAfterMigration),
	)

	// Create iterator over source database
	iter := source.NewIterator()
	defer iter.Release()

	// Create batch for destination
	batch := dest.NewBatch()
	batchCount := 0

	// Progress reporting ticker
	var progressTicker *time.Ticker
	if config.ProgressInterval > 0 {
		progressTicker = time.NewTicker(config.ProgressInterval)
		defer progressTicker.Stop()
	}

	// Migrate all key-value pairs
	for iter.Next() {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return stats, fmt.Errorf("migration cancelled: %w", ctx.Err())
		default:
		}

		// Get key and value
		key := iter.Key()
		value := iter.Value()

		// Add to batch
		if err := batch.Put(key, value); err != nil {
			stats.ErrorCount++
			if !config.ContinueOnError {
				return stats, fmt.Errorf("failed to add key to batch: %w", err)
			}
			log.Warn("failed to add key to batch, continuing",
				zap.Error(err),
				zap.Int("keySize", len(key)),
			)
			continue
		}

		// Update stats
		stats.TotalKeys++
		stats.TotalBytes += uint64(len(key) + len(value))
		batchCount++

		// Write batch when full
		if batchCount >= config.BatchSize {
			if err := batch.Write(); err != nil {
				stats.ErrorCount++
				if !config.ContinueOnError {
					return stats, fmt.Errorf("failed to write batch: %w", err)
				}
				log.Warn("failed to write batch, continuing", zap.Error(err))
			}
			batch.Reset()
			batchCount = 0
		}

		// Report progress
		if progressTicker != nil {
			select {
			case <-progressTicker.C:
				elapsed := time.Since(stats.StartTime)
				log.Info("migration progress",
					zap.Uint64("keys", stats.TotalKeys),
					zap.Uint64("bytes", stats.TotalBytes),
					zap.Float64("keysPerSec", stats.KeysPerSecond()),
					zap.Float64("mbPerSec", stats.BytesPerSecond()/(1024*1024)),
					zap.Duration("elapsed", elapsed.Round(time.Second)),
					zap.Uint64("errors", stats.ErrorCount),
				)
				stats.LastUpdateTime = time.Now()
			default:
			}
		}
	}

	// Check for iterator errors
	if err := iter.Error(); err != nil {
		return stats, fmt.Errorf("iterator error during migration: %w", err)
	}

	// Write final batch
	if batchCount > 0 {
		if err := batch.Write(); err != nil {
			return stats, fmt.Errorf("failed to write final batch: %w", err)
		}
	}

	stats.EndTime = time.Now()

	log.Info("migration completed",
		zap.Uint64("totalKeys", stats.TotalKeys),
		zap.Uint64("totalBytes", stats.TotalBytes),
		zap.Duration("duration", stats.EndTime.Sub(stats.StartTime).Round(time.Second)),
		zap.Float64("keysPerSec", stats.KeysPerSecond()),
		zap.Float64("mbPerSec", stats.BytesPerSecond()/(1024*1024)),
		zap.Uint64("errors", stats.ErrorCount),
	)

	// Optional verification
	if config.VerifyAfterMigration {
		log.Info("verifying migrated data")
		if err := verifyMigration(ctx, source, dest, log); err != nil {
			return stats, fmt.Errorf("migration verification failed: %w", err)
		}
		log.Info("migration verification passed")
	}

	return stats, nil
}

// verifyMigration verifies that all data from source exists in destination
func verifyMigration(
	ctx context.Context,
	source database.Database,
	dest database.Database,
	log logging.Logger,
) error {
	iter := source.NewIterator()
	defer iter.Release()

	verified := uint64(0)
	errors := uint64(0)

	for iter.Next() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("verification cancelled: %w", ctx.Err())
		default:
		}

		key := iter.Key()
		sourceValue := iter.Value()

		// Check if key exists in destination
		has, err := dest.Has(key)
		if err != nil {
			return fmt.Errorf("failed to check key existence: %w", err)
		}
		if !has {
			errors++
			log.Error("key missing in destination",
				zap.Int("keySize", len(key)),
			)
			continue
		}

		// Verify value matches
		destValue, err := dest.Get(key)
		if err != nil {
			return fmt.Errorf("failed to get value from destination: %w", err)
		}

		if !bytesEqual(sourceValue, destValue) {
			errors++
			log.Error("value mismatch",
				zap.Int("keySize", len(key)),
				zap.Int("sourceValueSize", len(sourceValue)),
				zap.Int("destValueSize", len(destValue)),
			)
			continue
		}

		verified++
		if verified%100000 == 0 {
			log.Info("verification progress", zap.Uint64("verified", verified))
		}
	}

	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterator error during verification: %w", err)
	}

	if errors > 0 {
		return fmt.Errorf("verification failed: %d errors found", errors)
	}

	log.Info("verification complete", zap.Uint64("verified", verified))
	return nil
}

// bytesEqual compares two byte slices
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

// EstimateMigrationTime estimates how long migration will take
//
// This samples a portion of the source database to estimate:
// - Total number of keys
// - Total data size
// - Expected migration duration
//
// The estimate assumes migration throughput matches the sample throughput.
func EstimateMigrationTime(
	ctx context.Context,
	source database.Database,
	sampleSize int,
	log logging.Logger,
) (keys uint64, bytes uint64, estimatedDuration time.Duration, err error) {
	if sampleSize <= 0 {
		return 0, 0, 0, fmt.Errorf("sampleSize must be positive")
	}

	iter := source.NewIterator()
	defer iter.Release()

	startTime := time.Now()
	sampled := 0
	totalBytes := uint64(0)

	// Sample first N keys
	for iter.Next() && sampled < sampleSize {
		select {
		case <-ctx.Done():
			return 0, 0, 0, ctx.Err()
		default:
		}

		key := iter.Key()
		value := iter.Value()
		totalBytes += uint64(len(key) + len(value))
		sampled++
	}

	if err := iter.Error(); err != nil {
		return 0, 0, 0, err
	}

	if sampled == 0 {
		return 0, 0, 0, nil
	}

	// Calculate sample throughput
	sampleDuration := time.Since(startTime)
	keysPerSecond := float64(sampled) / sampleDuration.Seconds()

	// Estimate total by counting remaining keys
	remaining := 0
	for iter.Next() {
		remaining++
		if remaining%100000 == 0 {
			log.Info("counting remaining keys", zap.Int("counted", remaining))
		}
	}

	totalKeys := uint64(sampled + remaining)
	avgBytesPerKey := float64(totalBytes) / float64(sampled)
	estimatedBytes := uint64(float64(totalKeys) * avgBytesPerKey)
	estimatedSeconds := float64(totalKeys) / keysPerSecond
	estimatedDuration = time.Duration(estimatedSeconds * float64(time.Second))

	log.Info("migration estimate",
		zap.Uint64("totalKeys", totalKeys),
		zap.Uint64("totalBytes", estimatedBytes),
		zap.Duration("estimatedDuration", estimatedDuration.Round(time.Second)),
		zap.Float64("keysPerSec", keysPerSecond),
	)

	return totalKeys, estimatedBytes, estimatedDuration, nil
}
