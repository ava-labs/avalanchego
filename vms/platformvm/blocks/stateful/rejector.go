// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

var _ Rejector = &rejector{}

type Rejector interface {
	rejectProposalBlock(b *ProposalBlock) error
	rejectAtomicBlock(b *AtomicBlock) error
	rejectStandardBlock(b *StandardBlock) error
	rejectCommitBlock(b *CommitBlock) error
	rejectAbortBlock(b *AbortBlock) error
}

func NewRejector() Rejector {
	// TODO implement
	return &rejector{}
}

type rejector struct {
	backend
}

func (r *rejector) rejectProposalBlock(b *ProposalBlock) error {
	b.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting Proposal Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	b.onCommitState = nil
	b.onAbortState = nil

	if err := r.add(b.Tx); err != nil {
		b.txExecutorBackend.Ctx.Log.Verbo(
			"failed to reissue tx %q due to: %s",
			b.Tx.ID(),
			err,
		)
	}

	defer b.reject()
	r.addStatelessBlock(b.ProposalBlock, b.Status())
	return r.commit()
}

func (r *rejector) rejectAtomicBlock(b *AtomicBlock) error {
	b.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting Atomic Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	if err := r.add(b.Tx); err != nil {
		b.txExecutorBackend.Ctx.Log.Debug(
			"failed to reissue tx %q due to: %s",
			b.Tx.ID(),
			err,
		)
	}

	defer b.reject()
	r.addStatelessBlock(b.AtomicBlock, b.Status())
	return r.commit()
}

func (r *rejector) rejectStandardBlock(b *StandardBlock) error {
	b.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting Standard Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	for _, tx := range b.Txs {
		if err := r.add(tx); err != nil {
			b.txExecutorBackend.Ctx.Log.Debug(
				"failed to reissue tx %q due to: %s",
				tx.ID(),
				err,
			)
		}
	}

	defer b.reject()
	r.addStatelessBlock(b.StandardBlock, b.Status())
	return r.commit()
}

func (r *rejector) rejectCommitBlock(b *CommitBlock) error {
	b.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting CommitBlock Block %s at height %d with parent %s",
		b.ID(), b.Height(), b.Parent(),
	)

	defer b.reject()
	r.addStatelessBlock(b.CommitBlock, b.Status())
	return r.commit()
}

func (r *rejector) rejectAbortBlock(b *AbortBlock) error {
	b.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting Abort Block %s at height %d with parent %s",
		b.ID(),
		b.Height(),
		b.Parent(),
	)

	defer b.reject()
	r.addStatelessBlock(b.AbortBlock, b.Status())
	return r.commit()
}
