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

	if err := r.Add(b.Tx); err != nil {
		r.ctx.Log.Verbo(
			"failed to reissue tx %q due to: %s",
			b.Tx.ID(),
			err,
		)
	}

	b.status = choices.Rejected
	defer b.free()
	r.AddStatelessBlock(b.ProposalBlock, choices.Rejected)
	return r.Commit()
}

func (r *rejectorImpl) rejectAtomicBlock(b *AtomicBlock) error {
	r.ctx.Log.Verbo(
		"Rejecting Atomic Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	if err := r.Add(b.Tx); err != nil {
		r.ctx.Log.Debug(
			"failed to reissue tx %q due to: %s",
			b.Tx.ID(),
			err,
		)
	}

	b.status = choices.Rejected
	defer b.free()
	r.AddStatelessBlock(b.AtomicBlock, choices.Rejected)
	return r.Commit()
}

func (r *rejectorImpl) rejectStandardBlock(b *StandardBlock) error {
	r.ctx.Log.Verbo(
		"Rejecting Standard Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	for _, tx := range b.Txs {
		if err := r.Add(tx); err != nil {
			r.ctx.Log.Debug(
				"failed to reissue tx %q due to: %s",
				tx.ID(),
				err,
			)
		}
	}

	b.status = choices.Rejected
	defer b.free()
	r.AddStatelessBlock(b.StandardBlock, choices.Rejected)
	return r.Commit()
}

func (r *rejectorImpl) rejectCommitBlock(b *CommitBlock) error {
	r.ctx.Log.Verbo(
		"Rejecting CommitBlock Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	b.status = choices.Rejected
	defer b.free()
	r.AddStatelessBlock(b.CommitBlock, choices.Rejected)
	return r.Commit()
}

func (r *rejectorImpl) rejectAbortBlock(b *AbortBlock) error {
	r.ctx.Log.Verbo(
		"Rejecting Abort Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	b.status = choices.Rejected
	defer b.free()
	r.AddStatelessBlock(b.AbortBlock, choices.Rejected)
	return r.Commit()
}
