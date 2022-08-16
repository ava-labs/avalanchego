// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"

	transactions "github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// forkBuilder defines how to create a new block given the current network version (fork).
// Different versions may have different block types, and different rules for
// block/transaction creation.
type forkBuilder interface {
	// Returns a new snowman.Block that includes transactions
	// selected based on the network version.
	// Only modifies state to remove expired proposal txs.
	buildBlock() (snowman.Block, []*transactions.Tx, error)
}
