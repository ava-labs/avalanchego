// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

var _ snowman.Block = &block{}

// TODO remove
// type Block interface {
// 	snowman.Block

// TODO remove
// returns true if this block or any processing ancestors consume any of the
// named atomic imports.
// conflicts(ids.Set) (bool, error)

// TODO remove
// addChild notifies this block that it has a child block building on it.
// When this block commits its changes, it should set the child's base state
// to the internal state. This ensures that the state versions do not
// recurse the length of the chain.
// addChild(Block)

// TODO remove
// free all the references of this block from the vm's memory
// free()

// Set the block's underlying state to the chain's internal state
// 	setBaseState()
// }

func NewBlock(
	blk stateless.Block,
	manager *manager,
) snowman.Block {
	return &block{
		manager: manager,
		Block:   blk,
	}
}

type block struct {
	stateless.Block
	manager *manager
}

func (b *block) Verify() error {
	return b.Visit(b.manager.verifier)
}

func (b *block) Accept() error {
	return b.Visit(b.manager.acceptor)
}

func (b *block) Reject() error {
	return b.Visit(b.manager.rejector)
}

// TODO
func (b *block) Status() choices.Status {
	blkID := b.ID()
	// Check if the block is in memory
	if _, ok := b.manager.backend.blkIDToState[blkID]; ok {
		return choices.Processing
	}
	// Block isn't in memory. Check in the database.
	_, status, err := b.manager.GetStatelessBlock(blkID)
	if err != nil {
		// It isn't in the database.
		// TODO is this right?
		return choices.Processing
	}
	return status
}

// TODO
func (b *block) Timestamp() time.Time {
	// 	 If this is the last accepted block and the block was loaded from disk
	// 	 since it was accepted, then the timestamp wouldn't be set correctly. So,
	// 	 we explicitly return the chain time.
	//if c.baseBlk.ID() == c.backend.GetLastAccepted() {
	//	return c.GetTimestamp()
	//}
	blkID := b.ID()
	// Check if the block is processing.
	if blkState, ok := b.manager.blkIDToState[blkID]; ok {
		return blkState.timestamp
	}
	// The block isn't processing.
	// According to the snowman.Block interface, the last accepted
	// block is the only accepted block that must return a correct timestamp,
	// so we just return the chain time.
	return b.manager.state.GetTimestamp()
}

// TODO rename
type blockState struct {
	// TODO add stateless block to this struct
	statelessBlock         stateless.Block
	status                 choices.Status
	onAcceptFunc           func()
	onAcceptState          state.Diff
	onCommitState          state.Diff
	onAbortState           state.Diff
	children               []ids.ID
	timestamp              time.Time
	inputs                 ids.Set
	atomicRequests         map[ids.ID]*atomic.Requests
	inititallyPreferCommit bool
}
