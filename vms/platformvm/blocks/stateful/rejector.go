// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var _ stateless.Visitor = &rejector{}

type rejector struct {
	backend
}

func (r *rejector) VisitProposalBlock(b *stateless.ProposalBlock) error {
	blkID := b.ID()

	r.ctx.Log.Verbo(
		"Rejecting Proposal Block %s at height %d with parent %s",
		blkID,
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

	defer r.free(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) VisitAtomicBlock(b *stateless.AtomicBlock) error {
	blkID := b.ID()

	r.ctx.Log.Verbo(
		"Rejecting Atomic Block %s at height %d with parent %s",
		blkID,
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

	defer r.free(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) VisitStandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()

	r.ctx.Log.Verbo(
		"Rejecting Standard Block %s at height %d with parent %s",
		blkID,
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

	defer r.free(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) VisitCommitBlock(b *stateless.CommitBlock) error {
	blkID := b.ID()

	r.ctx.Log.Verbo(
		"Rejecting CommitBlock Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	defer func() {
		r.free(blkID)
		r.free(b.Parent())
	}()
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) VisitAbortBlock(b *stateless.AbortBlock) error {
	blkID := b.ID()

	r.ctx.Log.Verbo(
		"Rejecting Abort Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	defer func() {
		r.free(blkID)
		r.free(b.Parent())
	}()
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}
