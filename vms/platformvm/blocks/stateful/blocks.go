// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

var ErrBlockNil = errors.New("block is nil")

type Block interface {
	snowman.Block

	// ExpectedChildVersion returns the expected version of this block's
	// direct child
	ExpectedChildVersion() uint16

	// parent returns the parent block, similarly to Parent. However, it
	// provides the more specific Block interface.
	parentBlock() (Block, error)

	// returns true if this block or any processing ancestors consume any of the
	// named atomic imports.
	conflicts(ids.Set) (bool, error)

	// addChild notifies this block that it has a child block building on it.
	// When this block commits its changes, it should set the child's base state
	// to the internal state. This ensures that the state versions do not
	// recurse the length of the chain.
	addChild(Block)

	// free all the references of this block from the vm's memory
	free()

	// Set the block's underlying state to the chain's internal state
	setBaseState()
}

// A Decision block (either Commit, Abort, or DecisionBlock) represents a
// Decision to either commit or abort the changes specified in its parent,
// if its parent is a proposal. Otherwise, the changes are committed
// immediately.
type Decision interface {
	// This function should only be called after Verify is called.
	// OnAccept returns:
	// 1) The current state of the chain, if this block is decided or hasn't
	//    been verified.
	// 2) The state of the chain after this block is accepted, if this block was
	//    verified successfully.
	OnAccept() state.Chain
}
