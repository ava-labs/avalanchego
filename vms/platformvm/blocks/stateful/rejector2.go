// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var _ stateless.BlockRejector = &rejector2{}

type rejector2 struct {
	backend
}

func (r *rejector2) RejectProposalBlock(b *stateless.ProposalBlock) error {
	blkID := b.ID()
	blockState, ok := r.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("block %s state not found", blkID)
	}

	r.ctx.Log.Verbo(
		"Rejecting Proposal Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	if err := r.Mempool.Add(b.Tx); err != nil {
		r.ctx.Log.Verbo(
			"failed to reissue tx %q due to: %s",
			b.Tx.ID(),
			err,
		)
	}

	// TODO remove
	// b.status = choices.Rejected
	blockState.status = choices.Rejected
	//r.blkIDToStatus[blkID] = choices.Rejected
	defer r.free(blkID)
	r.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector2) RejectAtomicBlock(b *stateless.AtomicBlock) error {
	blkID := b.ID()
	blockState, ok := r.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("block %s state not found", blkID)
	}

	r.ctx.Log.Verbo(
		"Rejecting Atomic Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	if err := r.Mempool.Add(b.Tx); err != nil {
		r.ctx.Log.Debug(
			"failed to reissue tx %q due to: %s",
			b.Tx.ID(),
			err,
		)
	}

	// TODO remove
	// b.status = choices.Rejected
	// 	r.blkIDToStatus[blkID] = choices.Rejected
	blockState.status = choices.Rejected
	defer r.free(blkID)
	r.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector2) RejectStandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()
	blockState, ok := r.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("block %s state not found", blkID)
	}

	r.ctx.Log.Verbo(
		"Rejecting Standard Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	for _, tx := range b.Txs {
		if err := r.Mempool.Add(tx); err != nil {
			r.ctx.Log.Debug(
				"failed to reissue tx %q due to: %s",
				tx.ID(),
				err,
			)
		}
	}

	// TODO remove
	// b.status = choices.Rejected
	// 	r.blkIDToStatus[blkID] = choices.Rejected
	blockState.status = choices.Rejected
	defer r.free(b.ID())
	r.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector2) RejectCommitBlock(b *stateless.CommitBlock) error {
	blkID := b.ID()
	blockState, ok := r.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("block %s state not found", blkID)
	}

	r.ctx.Log.Verbo(
		"Rejecting CommitBlock Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	// b.status = choices.Rejected
	// 	r.blkIDToStatus[blkID] = choices.Rejected
	blockState.status = choices.Rejected

	defer r.free(b.ID())
	r.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector2) RejectAbortBlock(b *stateless.AbortBlock) error {
	blkID := b.ID()
	blockState, ok := r.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("block %s state not found", blkID)
	}

	r.ctx.Log.Verbo(
		"Rejecting Abort Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	// b.status = choices.Rejected
	// r.blkIDToStatus[blkID] = choices.Rejected
	blockState.status = choices.Rejected
	defer r.free(b.ID())
	r.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}
