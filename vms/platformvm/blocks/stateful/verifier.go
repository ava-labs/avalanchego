// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/mempool"

	p_tx "github.com/ava-labs/avalanchego/vms/platformvm/transactions/executor"
)

type Verifier interface {
	mempool.Mempool
	state.State
	p_tx.Executor
	stateless.Metrics

	GetStatefulBlock(blkID ids.ID) (Block, error)
	CacheVerifiedBlock(Block)
	DropVerifiedBlock(blkID ids.ID)

	// register recently accepted blocks, needed
	// to calculate the minimum height of the block still in the
	// Snowman++ proposal window.
	AddToRecentlyAcceptedWindows(blkID ids.ID)
}
