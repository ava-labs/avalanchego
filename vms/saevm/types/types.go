// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package types defines common types that are used throughout the SAE
// repository which otherwise do not have a clear package to originate from.
//
// Imports of other packages should be extremely limited to avoid circular
// dependencies.
package types

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/database"
)

type (
	// A BlockSource returns a Block that matches both a hash and number, and a
	// boolean indicating if such a block was found.
	BlockSource func(hash common.Hash, number uint64) (*types.Block, bool)
	// A HeaderSource is equivalent to a [BlockSource] except that it only
	// returns the block header.
	HeaderSource func(hash common.Hash, number uint64) (*types.Header, bool)
)

// ExecutionResults provides type safety for a [database.HeightIndex], to be
// used for persistence of SAE-specific execution results, avoiding possible
// collision with `rawdb` keys.
type ExecutionResults struct {
	database.HeightIndex
}
