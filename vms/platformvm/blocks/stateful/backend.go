// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
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

func (b *backend) ExpectedChildVersion(blk snowman.Block) uint16 {
	return b.expectedChildVersion(blk.Timestamp())
}

func (b *backend) expectedChildVersion(blkTime time.Time) uint16 {
	forkTime := b.cfg.BlueberryTime
	if blkTime.Before(forkTime) {
		return stateless.ApricotVersion
	}
	return version.BlueberryBlockVersion
}

// Note: free may be invoked multiple time on the same block
// (e.g. twice on any option's parent). Hence it's important
// to make sure free stays idempotent.
func (b *backend) free(blkID ids.ID) {
	delete(b.blkIDToState, blkID)
}

func (b *backend) getStatelessBlock(blkID ids.ID) (stateless.Block, error) {
	// See if the block is in memory.
	if blk, ok := b.blkIDToState[blkID]; ok {
		return blk.statelessBlock, nil
	}
	// The block isn't in memory. Check the database.
	statelessBlk, _, err := b.state.GetStatelessBlock(blkID)
	return statelessBlk, err
}
