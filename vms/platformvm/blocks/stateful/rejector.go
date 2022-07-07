// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import "github.com/ava-labs/avalanchego/snow/choices"

var _ rejector = &rejectorImpl{}

type rejector interface {
	rejectProposalBlock(b *ProposalBlock) error
	rejectAtomicBlock(b *AtomicBlock) error
	rejectStandardBlock(b *StandardBlock) error
	rejectCommitBlock(b *CommitBlock) error
	rejectAbortBlock(b *AbortBlock) error
}

type rejectorImpl struct {
	backend
}

func (r *rejectorImpl) rejectProposalBlock(b *ProposalBlock) error {
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
	blkID := b.ID()
	r.blkIDToStatus[blkID] = choices.Rejected
	defer r.free(blkID)
	r.AddStatelessBlock(b.ProposalBlock, choices.Rejected)
	return r.state.Commit()
}

func (r *rejectorImpl) rejectAtomicBlock(b *AtomicBlock) error {
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
	blkID := b.ID()
	r.blkIDToStatus[blkID] = choices.Rejected
	defer r.free(blkID)
	r.AddStatelessBlock(b.AtomicBlock, choices.Rejected)
	return r.state.Commit()
}

func (r *rejectorImpl) rejectStandardBlock(b *StandardBlock) error {
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
	blkID := b.ID()
	r.blkIDToStatus[blkID] = choices.Rejected
	defer r.free(b.ID())
	r.AddStatelessBlock(b.StandardBlock, choices.Rejected)
	return r.state.Commit()
}

func (r *rejectorImpl) rejectCommitBlock(b *CommitBlock) error {
	r.ctx.Log.Verbo(
		"Rejecting CommitBlock Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	// b.status = choices.Rejected
	blkID := b.ID()
	r.blkIDToStatus[blkID] = choices.Rejected
	defer r.free(b.ID())
	r.AddStatelessBlock(b.CommitBlock, choices.Rejected)
	return r.state.Commit()
}

func (r *rejectorImpl) rejectAbortBlock(b *AbortBlock) error {
	r.ctx.Log.Verbo(
		"Rejecting Abort Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	// b.status = choices.Rejected
	blkID := b.ID()
	r.blkIDToStatus[blkID] = choices.Rejected
	defer r.free(b.ID())
	r.AddStatelessBlock(b.AbortBlock, choices.Rejected)
	return r.state.Commit()
}
