// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

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

// TODO rename
type stat struct {
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
