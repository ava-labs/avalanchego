// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

// TODO improve/add comments.
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
	blkIDToState map[ids.ID]*blockState
	state        state.State
	ctx          *snow.Context
	cfg          *config.Config
	bootstrapped *utils.AtomicBool
}

func (b *backend) OnAccept(blkID ids.ID) state.Chain {
	blockState, ok := b.blkIDToState[blkID]
	if !ok {
		return b.state
	}
	return blockState.onAcceptState
}

func (b *backend) ExpectedChildVersion(blk snowman.Block) uint16 {
	return b.expectedChildVersion(blk.Timestamp())
}

func (b *backend) expectedChildVersion(blkTime time.Time) uint16 {
	forkTime := b.cfg.BlueberryTime
	if blkTime.Before(forkTime) {
		return stateless.ApricotVersion
	}
	return stateless.BlueberryVersion
}

func (b *backend) free(blkID ids.ID) {
	delete(b.blkIDToState, blkID)
}
