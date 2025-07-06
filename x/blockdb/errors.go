// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidBlockHeight = errors.New("blockdb: invalid block height")
	ErrBlockEmpty         = errors.New("blockdb: block is empty")
	ErrDatabaseClosed     = errors.New("blockdb: database is closed")
	ErrCorrupted          = errors.New("blockdb: unrecoverable corruption detected")
	ErrHeaderSizeTooLarge = errors.New("blockdb: header size cannot be >= block size")
	ErrBlockTooLarge      = fmt.Errorf("blockdb: block size too large")
	ErrBlockNotFound      = errors.New("blockdb: block not found")
)
