// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var _ stateless.Visitor = &rejector{}

// rejector handles the logic for rejecting a block.
type rejector struct {
	*backend
}

func (r *rejector) VisitProposalBlock(b *stateless.ProposalBlock) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting Proposal Block %s at height %d with parent %s",
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

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) VisitAtomicBlock(b *stateless.AtomicBlock) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting Atomic Block %s at height %d with parent %s",
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

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) VisitStandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting Standard Block %s at height %d with parent %s",
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

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) VisitCommitBlock(b *stateless.CommitBlock) error {
	return r.rejectOptionBlock(b, true /* isCommit */)
}

func (r *rejector) VisitAbortBlock(b *stateless.AbortBlock) error {
	return r.rejectOptionBlock(b, false /* isCommit */)
}

func (r *rejector) rejectOptionBlock(b stateless.Block, isCommit bool) error {
	blkID := b.ID()
	defer r.free(blkID)

	if isCommit {
		r.ctx.Log.Verbo(
			"rejecting Commit Block %s at height %d with parent %s",
			blkID,
			b.Height(),
			b.Parent(),
		)
	} else {
		r.ctx.Log.Verbo(
			"rejecting Abort Block %s at height %d with parent %s",
			blkID,
			b.Height(),
			b.Parent(),
		)
	}

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}
