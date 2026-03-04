// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"errors"
)

var ErrNotOracle = errors.New("block isn't an oracle")

// OracleBlock is a block that only has two valid children. The children should
// be returned in preferential order.
//
// This ordering does not need to be deterministically created from the chain
// state.
type OracleBlock interface {
	// Options returns the possible children of this block in the order this
	// validator prefers the blocks.
	Options(context.Context) ([2]Block, error)
}
