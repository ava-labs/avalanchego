// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import "errors"

// DefaultMaxDataFileSize is the default maximum size of the data block file in bytes (500GB).
const DefaultMaxDataFileSize = 500 * 1024 * 1024 * 1024

// MaxDataFiles is the maximum number of data files that can be created.
// This prevents running out of file descriptors when MaxDataFileSize is small.
const MaxDataFiles = 10_000

// DatabaseConfig contains configuration parameters for BlockDB.
type DatabaseConfig struct {
	// MinimumHeight is the lowest block height tracked by the database.
	MinimumHeight uint64

	// MaxDataFileSize sets the maximum size of the data block file in bytes.
	MaxDataFileSize uint64

	// CheckpointInterval defines how frequently (in blocks) the index file header is updated (default: 1024).
	CheckpointInterval uint64

	// SyncToDisk determines if fsync is called after each write for durability.
	SyncToDisk bool

	// Truncate determines if existing data should be truncated when opening the database.
	Truncate bool
}

// DefaultDatabaseConfig returns the default options for BlockDB.
func DefaultDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		MinimumHeight:      0,
		MaxDataFileSize:    DefaultMaxDataFileSize,
		CheckpointInterval: 1024,
		SyncToDisk:         true,
		Truncate:           false,
	}
}

// WithSyncToDisk returns a copy of the config with SyncToDisk set to the given value.
func (c DatabaseConfig) WithSyncToDisk(syncToDisk bool) DatabaseConfig {
	c.SyncToDisk = syncToDisk
	return c
}

// WithTruncate returns a copy of the config with Truncate set to the given value.
func (c DatabaseConfig) WithTruncate(truncate bool) DatabaseConfig {
	c.Truncate = truncate
	return c
}

// WithMinimumHeight returns a copy of the config with MinimumHeight set to the given value.
func (c DatabaseConfig) WithMinimumHeight(minHeight uint64) DatabaseConfig {
	c.MinimumHeight = minHeight
	return c
}

// WithMaxDataFileSize returns a copy of the config with MaxDataFileSize set to the given value.
func (c DatabaseConfig) WithMaxDataFileSize(maxSize uint64) DatabaseConfig {
	c.MaxDataFileSize = maxSize
	return c
}

// WithCheckpointInterval returns a copy of the config with CheckpointInterval set to the given value.
func (c DatabaseConfig) WithCheckpointInterval(interval uint64) DatabaseConfig {
	c.CheckpointInterval = interval
	return c
}

// Validate checks if the store options are valid.
func (c DatabaseConfig) Validate() error {
	if c.CheckpointInterval == 0 {
		return errors.New("CheckpointInterval cannot be 0")
	}
	return nil
}
