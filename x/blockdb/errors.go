// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import "errors"

var (
	ErrInvalidBlockHeight = errors.New("blockdb: invalid block height")
	ErrCorrupted          = errors.New("blockdb: unrecoverable corruption detected")
	ErrBlockTooLarge      = errors.New("blockdb: block size too large")
)
