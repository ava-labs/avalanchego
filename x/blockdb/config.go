// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import "errors"

// DefaultMaxDataFileSize is the default maximum size of the data block file in bytes (500GB).
const DefaultMaxDataFileSize = 500 * 1024 * 1024 * 1024

// DatabaseConfig contains configuration parameters for BlockDB.
type DatabaseConfig struct {
	// MinimumHeight is the lowest block height tracked by the database.
	MinimumHeight uint64

	// MaxDataFileSize sets the maximum size of the data block file in bytes. If 0, there is no limit.
	MaxDataFileSize uint64

	// CheckpointInterval defines how frequently (in blocks) the index file header is updated (default: 1024).
	CheckpointInterval uint64
}

// DefaultDatabaseConfig returns the default options for BlockDB.
func DefaultDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		MinimumHeight:      0,
		MaxDataFileSize:    DefaultMaxDataFileSize,
		CheckpointInterval: 1024,
	}
}

// Validate checks if the store options are valid.
func (opts DatabaseConfig) Validate() error {
	if opts.CheckpointInterval == 0 {
		return errors.New("CheckpointInterval cannot be 0")
	}
	return nil
}
