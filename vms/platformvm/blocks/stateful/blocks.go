// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

var _ snowman.Block = &block{}

type Block interface {
	snowman.Block

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
}

func NewBlock(blk stateless.Block, manager Manager) snowman.Block {
	return &block{
		manager: manager,
		Block:   blk,
	}
}

type block struct {
	stateless.Block
	manager Manager
}

func (b *block) Verify() error {
	return b.Visit(b.manager.verifyVisitor)
}

func (b *block) Accept() error {
	return errors.New("TODO")
}

func (b *block) Reject() error {
	return errors.New("TODO")
}

// TODO
func (b *block) Status() choices.Status {
	return choices.Unknown
}

// TODO
func (b *block) Timestamp() time.Time {
	return time.Time{}
}

// TODO rename
type blockState struct {
	// TODO add stateless block to this struct
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
