// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

type lastAccepteder interface {
	SetLastAccepted(blkID ids.ID)
	GetLastAccepted() ids.ID
}

type versionDB interface {
	Abort()
	CommitBatch() (database.Batch, error)
	Commit() error
}

type heightSetter interface {
	SetHeight(height uint64)
}

type backend struct {
	mempool.Mempool
	metrics.Metrics
	// TODO consolidate state fields below?
	versionDB
	lastAccepteder
	blockState
	heightSetter
	state state.State
}

func (b *backend) getState() state.State {
	return b.state
}

// TODO do we even need this or can we just pass parent ID into getStatefulBlock?
func (b *backend) parent(blk *stateless.CommonBlock) (Block, error) {
	parentBlkID := blk.Parent()
	return b.GetStatefulBlock(parentBlkID)
}
