// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import "errors"

var (
	ErrInvalidBlockHeight = errors.New("invalid block height")
	ErrCorrupted          = errors.New("unrecoverable corruption detected")
	ErrBlockTooLarge      = errors.New("block size too large")
)
