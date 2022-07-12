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

type blockState struct {
	statelessBlock         stateless.Block
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
