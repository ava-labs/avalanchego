// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

type heightSetter interface {
	SetHeight(height uint64)
}

// TODO improve/add comments.
// Shared fields used by visitors.
type backend struct {
	mempool.Mempool
	// TODO consolidate state fields below?
	blockState
	heightSetter
	// Block ID --> Status of the block.
	blkIDToStatus map[ids.ID]choices.Status
	// Block ID --> Function to be executed if the block is accepted.
	blkIDToOnAcceptFunc map[ids.ID]func()
	// Block ID --> State if the block is accepted.
	blkIDToOnAcceptState map[ids.ID]state.Diff
	// Block ID --> State if this block's proposal is committed.
	blkIDToOnCommitState map[ids.ID]state.Diff
	// Block ID --> State if this block's proposal is aborted.
	blkIDToOnAbortState map[ids.ID]state.Diff
	// Block ID --> Children of that block.
	blkIDToChildren map[ids.ID][]Block
	// Block ID --> Time the block was proposed.
	blkIDToTimestamp map[ids.ID]time.Time
	// Block ID --> Inputs of txs in that block.
	blkIDToInputs map[ids.ID]ids.Set
	// Block ID --> Atomic requests for txs in that block.
	blkIDToAtomicRequests map[ids.ID]map[ids.ID]*atomic.Requests
	// Block ID --> Whether we initially prefer committing this block.
	blkIDToPreferCommit map[ids.ID]bool
	state               state.State
	ctx                 *snow.Context
	bootstrapped        *utils.AtomicBool
}

func (b *backend) getState() state.State {
	return b.state
}

// TODO do we even need this or can we just pass parent ID into getStatefulBlock?
func (b *backend) parent(blk *stateless.CommonBlock) (Block, error) {
	parentBlkID := blk.Parent()
	return b.GetStatefulBlock(parentBlkID)
}

func (b *backend) OnAccept(blkID ids.ID) state.Chain {
	onAcceptState := b.blkIDToOnAcceptState[blkID]
	// TODO is the below correct?
	// TODO remove or fix commented code below.
	if /*blk.Status().Decided() ||*/ onAcceptState == nil {
		return b.state
	}
	return onAcceptState
}

func (b *backend) free(blkID ids.ID) {
	delete(b.blkIDToOnAcceptFunc, blkID)
	delete(b.blkIDToOnAcceptState, blkID)
	delete(b.blkIDToOnAcceptState, blkID)
	delete(b.blkIDToOnAbortState, blkID)
	delete(b.blkIDToChildren, blkID)
	delete(b.blkIDToTimestamp, blkID)
	delete(b.blkIDToInputs, blkID)
	delete(b.blkIDToAtomicRequests, blkID)
	delete(b.blkIDToPreferCommit, blkID)
	b.unpinVerifiedBlock(blkID)
}
