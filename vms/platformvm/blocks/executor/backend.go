// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

// Shared fields used by visitors.
type backend struct {
	mempool.Mempool
	// lastAccepted is the ID of the last block that had Accept() called on it.
	lastAccepted ids.ID

	// blkIDToState is a map from a block's ID to the state of the block.
	// Blocks are put into this map when they are verified.
	// Proposal blocks are removed from this map when they are rejected
	// or when a child is accepted.
	// All other blocks are removed when they are accepted/rejected.
	// Note that Genesis block is a commit block so no need to update
	// blkIDToState with it upon backend creation (Genesis is already accepted)
	blkIDToState map[ids.ID]*blockState
	state        state.State

	ctx *snow.Context
}

func (b *backend) GetState(blkID ids.ID) (state.Chain, bool) {
	// If the block is in the map, it is either processing or a proposal block
	// that was accepted without an accepted child.
	if state, ok := b.blkIDToState[blkID]; ok {
		if state.onAcceptState != nil {
			return state.onAcceptState, true
		}
		return nil, false
	}

	// Note: If the last accepted block is a proposal block, we will have
	//       returned in the above if statement.
	return b.state, blkID == b.state.GetLastAccepted()
}

func (b *backend) getOnAbortState(blkID ids.ID) (state.Diff, bool) {
	state, ok := b.blkIDToState[blkID]
	if !ok || state.onAbortState == nil {
		return nil, false
	}
	return state.onAbortState, true
}

func (b *backend) getOnCommitState(blkID ids.ID) (state.Diff, bool) {
	state, ok := b.blkIDToState[blkID]
	if !ok || state.onCommitState == nil {
		return nil, false
	}
	return state.onCommitState, true
}

func (b *backend) GetBlock(blkID ids.ID) (blocks.Block, error) {
	// See if the block is in memory.
	if blk, ok := b.blkIDToState[blkID]; ok {
		return blk.statelessBlock, nil
	}
	// The block isn't in memory. Check the database.
	statelessBlk, _, err := b.state.GetStatelessBlock(blkID)
	return statelessBlk, err
}

func (b *backend) LastAccepted() ids.ID {
	return b.lastAccepted
}

func (b *backend) free(blkID ids.ID) {
	delete(b.blkIDToState, blkID)
}

func (b *backend) getTimestamp(blkID ids.ID) time.Time {
	// Check if the block is processing.
	// If the block is processing, then we are guaranteed to have populated its
	// timestamp in its state.
	if blkState, ok := b.blkIDToState[blkID]; ok {
		return blkState.timestamp
	}

	// The block isn't processing.
	// According to the snowman.Block interface, the last accepted
	// block is the only accepted block that must return a correct timestamp,
	// so we just return the chain time.
	return b.state.GetTimestamp()
}
