package blockdb

import "fmt"

var (
	ErrInvalidBlockHeight = fmt.Errorf("blockdb: invalid block height")
	ErrBlockEmpty         = fmt.Errorf("blockdb: block is empty")
	ErrDatabaseClosed     = fmt.Errorf("blockdb: database is closed")
	ErrCorrupted          = fmt.Errorf("blockdb: unrecoverable corruption detected")
	ErrBlockTooLarge      = fmt.Errorf("blockdb: block size exceeds maximum allowed size of %d bytes", MaxBlockDataSize)
	ErrHeaderSizeTooLarge = fmt.Errorf("blockdb: header size cannot be >= block size")
)
