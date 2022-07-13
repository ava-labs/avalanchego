// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

type standardBlockState struct {
	onAcceptFunc func()
}

type atomicBlockState struct {
	inputs ids.Set
}

type proposalBlockState struct {
	initiallyPreferCommit bool
	onCommitState         state.Diff
	onAbortState          state.Diff
}

// The state of a block.
// Note that not all fields will be set for a given block.
type blockState struct {
	standardBlockState
	proposalBlockState
	atomicBlockState
	statelessBlock stateless.Block
	onAcceptState  state.Diff
	// This block's children which have passed verification.
	children       []ids.ID
	timestamp      time.Time
	atomicRequests map[ids.ID]*atomic.Requests

	// Following Blueberry activation, onBlueberryBaseOptionsState is the base state
	// over which both commit and abort states are built
	// TODO ABENEGIA: should this be instead onAcceptState for ProposalBlock as well?
	// it would fit standard block flow
	onBlueberryBaseOptionsState state.Diff
}
