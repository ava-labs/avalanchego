// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/MetalBlockchain/metalgo/chains/atomic"
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/utils/set"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/block"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/metrics"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/state"
)

type proposalBlockState struct {
	onDecisionState *state.Diff
	onCommitState   *state.Diff
	onAbortState    *state.Diff
}

// The state of a block.
// Note that not all fields will be set for a given block.
type blockState struct {
	proposalBlockState
	statelessBlock block.Block

	onAcceptState *state.Diff
	onAcceptFunc  func()

	inputs          set.Set[ids.ID]
	timestamp       time.Time
	atomicRequests  map[ids.ID]*atomic.Requests
	verifiedHeights set.Set[uint64]
	metrics         metrics.Block
}
