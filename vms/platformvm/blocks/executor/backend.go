// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/forks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
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
	blkIDToState  map[ids.ID]*blockState
	state         state.State
	stateVersions state.Versions

	ctx          *snow.Context
	cfg          *config.Config
	bootstrapped *utils.AtomicBool
}

func (b *backend) GetFork(blkID ids.ID) (forks.Fork, error) {
	// We need the parent's timestamp.
	// Verify was already called on the parent (guaranteed by consensus engine).
	// The parent hasn't been rejected (guaranteed by consensus engine).
	// If the parent is accepted, the parent is the most recently
	// accepted block.
	// If the parent hasn't been accepted, the parent is in memory.
	var parentTimestamp time.Time
	if parentState, ok := b.blkIDToState[blkID]; ok {
		parentTimestamp = parentState.timestamp
	} else {
		parentTimestamp = b.state.GetTimestamp()
	}

	forkTime := b.cfg.BlueberryTime
	if parentTimestamp.Before(forkTime) {
		return forks.Apricot, nil
	}
	return forks.Blueberry, nil
}

func (b *backend) free(blkID ids.ID) {
	delete(b.blkIDToState, blkID)
}

func (b *backend) getStatelessBlock(blkID ids.ID) (blocks.Block, error) {
	// See if the block is in memory.
	if blk, ok := b.blkIDToState[blkID]; ok {
		return blk.statelessBlock, nil
	}
	// The block isn't in memory. Check the database.
	statelessBlk, _, err := b.state.GetStatelessBlock(blkID)
	return statelessBlk, err
}
