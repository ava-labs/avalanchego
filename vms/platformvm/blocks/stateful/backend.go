// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

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
	abort()
	commitBatch() (database.Batch, error)
	commit() error
}

type heightSetter interface {
	SetHeight(height uint64)
}

type backend struct {
	mempool.Mempool
	metrics.Metrics
	versionDB
	lastAccepteder
	blockState
	heightSetter
}

func (b *backend) markAccepted(blk stateless.Block) error {
	// TODO implement
	return errors.New("TODO")
}

func (b *backend) getState() state.State {
	// TODO
	return nil
}

// TODO do we even need this or can we just pass parent ID into getStatefulBlock?
func (b *backend) parent(blk *stateless.CommonBlock) (Block, error) {
	parentBlkID := blk.Parent()
	return b.getStatefulBlock(parentBlkID)
}

// TODO implement
// TODO specify what type block should be
func (b *backend) onAccept(blk Block) state.Chain {
	return nil
}
