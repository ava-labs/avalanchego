package blockdb

import (
	"fmt"
)

// DatabaseConfig contains configuration parameters for BlockDB.
type DatabaseConfig struct {
	// MinimumHeight is the lowest block height the store will track (must be >= 1).
	MinimumHeight uint64

	// MaxDataFileSize sets the maximum size of the data block file in bytes. If 0, there is no limit.
	MaxDataFileSize uint64

	// CheckpointInterval defines how frequently (in blocks) the index file header is updated (default: 1024).
	CheckpointInterval uint64
}

// DefaultDatabaseConfig returns the default options for BlockDB.
func DefaultDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		MinimumHeight:      1,
		MaxDataFileSize:    1 << 31, // Default to 2GB
		CheckpointInterval: 1024,
	}
}

// Validate checks if the store options are valid.
func (opts DatabaseConfig) Validate() error {
	if opts.MinimumHeight == 0 {
		return fmt.Errorf("%w: MinimumHeight cannot be 0, must be >= 1", ErrInvalidBlockHeight)
	}

	if opts.CheckpointInterval == 0 {
		return fmt.Errorf("%w: CheckpointInterval cannot be 0", ErrInvalidCheckpointInterval)
	}
	return nil
}
