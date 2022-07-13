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

	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) VisitCommitBlock(b *stateless.CommitBlock) error {
	blkID := b.ID()
	defer func() {
		r.free(blkID)
		// Note that it's OK to free the parent here.
		// We're accepting a Commit block, so its sibling, an Abort block,
		// must be accepted. (This is the only reason we'd reject this block.)
		// So it's OK to remove the parent's state from [r.blkIDToState] --
		// this block's sibling doesn't need it anymore.
		r.free(b.Parent())
	}()

	r.ctx.Log.Verbo(
		"rejecting Commit Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) VisitAbortBlock(b *stateless.AbortBlock) error {
	blkID := b.ID()
	defer func() {
		r.free(blkID)
		// Note that it's OK to free the parent here.
		// We're accepting an Abort block, so its sibling, a Commit block,
		// must be accepted. (This is the only reason we'd reject this block.)
		// So it's OK to remove the parent's state from [r.blkIDToState] --
		// this block's sibling doesn't need it anymore.
		r.free(b.Parent())
	}()

	r.ctx.Log.Verbo(
		"rejecting Abort Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}
