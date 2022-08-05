// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

// Shared fields used by visitors.
type backend struct {
	mempool.Mempool
	// Keep the last accepted block in memory because when we check a
	// proposal block's status, it may be accepted but not have an accepted
	// child, in which case it's in [blkIDToState].
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

	ctx          *snow.Context
	bootstrapped *utils.AtomicBool
}

func (b *backend) GetState(blkID ids.ID) (state.Chain, bool) {
	if blkID == b.lastAccepted {
		return b.state, true
	}

	state, ok := b.blkIDToState[blkID]
	if !ok {
		return nil, false
	}

	if state.onAcceptState != nil {
		return state.onAcceptState, true
	}
	return nil, false
}

func (b *backend) LastAccepted() ids.ID {
	return b.lastAccepted
}

func (b *backend) free(blkID ids.ID) {
	delete(b.blkIDToState, blkID)
}
