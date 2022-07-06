// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	ErrConflictingParentTxs = errors.New("block contains a transaction that conflicts with a transaction in a parent block")

	_ Block = &AtomicBlock{}
	// TODO remove
	// _ Decision = &AtomicBlock{}
)

// AtomicBlock being accepted results in the atomic transaction contained in the
// block to be accepted and committed to the chain.
type AtomicBlock struct {
	*stateless.AtomicBlock
	*decisionBlock

	// inputs are the atomic inputs that are consumed by this block's atomic
	// transaction
	inputs         ids.Set
	atomicRequests map[ids.ID]*atomic.Requests

	manager Manager
}

// NewAtomicBlock returns a new *AtomicBlock where the block's parent, a
// decision block, has ID [parentID].
func NewAtomicBlock(
	manager Manager,
	ctx *snow.Context,
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*AtomicBlock, error) {
	statelessBlk, err := stateless.NewAtomicBlock(parentID, height, tx)
	if err != nil {
		return nil, err
	}
	return toStatefulAtomicBlock(statelessBlk, manager, ctx, choices.Processing)
}

func toStatefulAtomicBlock(
	statelessBlk *stateless.AtomicBlock,
	manager Manager,
	ctx *snow.Context,
	status choices.Status,
) (*AtomicBlock, error) {
	ab := &AtomicBlock{
		AtomicBlock: statelessBlk,
		decisionBlock: &decisionBlock{
			chainState: manager,
			commonBlock: &commonBlock{
				timestampGetter: manager,
				LastAccepteder:  manager,
				baseBlk:         &statelessBlk.CommonBlock,
				status:          status,
			},
		},
		manager: manager,
	}

	ab.Tx.Unsigned.InitCtx(ctx)
	return ab, nil
}

// conflicts checks to see if the provided input set contains any conflicts with
// any of this block's non-accepted ancestors or itself.
func (ab *AtomicBlock) conflicts(s ids.Set) (bool, error) {
	return ab.manager.conflictsAtomicBlock(ab, s)
}

func (ab *AtomicBlock) Verify() error {
	return ab.manager.verifyAtomicBlock(ab)
}

func (ab *AtomicBlock) Accept() error {
	return ab.manager.acceptAtomicBlock(ab)
}

func (ab *AtomicBlock) Reject() error {
	return ab.manager.rejectAtomicBlock(ab)
}

func (ab *AtomicBlock) free() {
	ab.manager.freeAtomicBlock(ab)
}

func (ab *AtomicBlock) setBaseState() {
	ab.manager.setBaseStateAtomicBlock(ab)
}
