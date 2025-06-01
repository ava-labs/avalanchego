package blockdb

import "fmt"

var (
	ErrInvalidBlockHeight        = fmt.Errorf("blockdb: invalid block height")
	ErrBlockNotFound             = fmt.Errorf("blockdb: block not found")
	ErrBlockEmpty                = fmt.Errorf("blockdb: block is empty")
	ErrBlockSizeMismatch         = fmt.Errorf("blockdb: block size in index file does not match data header")
	ErrChecksumMismatch          = fmt.Errorf("blockdb: checksum mismatch")
	ErrStoreClosed               = fmt.Errorf("blockdb: store is closed")
	ErrInvalidCheckpointInterval = fmt.Errorf("blockdb: invalid checkpoint interval")
	ErrCorrupted                 = fmt.Errorf("blockdb: unrecoverable corruption detected")
	ErrBlockTooLarge             = fmt.Errorf("blockdb: block size exceeds maximum allowed size of %d bytes", MaxBlockDataSize)
)
