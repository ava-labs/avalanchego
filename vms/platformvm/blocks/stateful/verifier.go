// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/metadata"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

type Verifier interface {
	mempool.Mempool
	stateless.Metrics

	metadata.Content
	GetStatelessBlock(blockID ids.ID) (stateless.Block, choices.Status, error)
	AddStatelessBlock(block stateless.Block, status choices.Status)
	StateContentForApply() state.Content
	GetMutableState() state.Mutable
	Abort()
	Commit() error
	CommitBatch() (database.Batch, error)

	GetStatefulBlock(blkID ids.ID) (Block, error)
	CacheVerifiedBlock(Block)
	DropVerifiedBlock(blkID ids.ID)

	// register recently accepted blocks, needed
	// to calculate the minimum height of the block still in the
	// Snowman++ proposal window.
	AddToRecentlyAcceptedWindows(blkID ids.ID)
}
