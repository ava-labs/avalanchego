// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanche-go/snow/consensus/snowman"
)

// OracleBlock is a block that only has two valid children. The children should
// be returned in preferential order.
//
// This ordering does not need to be deterministically created from the chain
// state.
type OracleBlock interface {
	snowman.Block

	// Options returns the possible children of this block in the order this
	// validator prefers the blocks.
	Options() ([2]snowman.Block, error)
}
