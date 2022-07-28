// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"go.uber.org/zap"
)

var _ stateless.Visitor = &rejector{}

// rejector handles the logic for rejecting a block.
type rejector struct {
	*backend
}

func (r *rejector) ProposalBlock(b *stateless.ProposalBlock) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting Proposal Block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)

	if err := r.Mempool.Add(b.Tx); err != nil {
		r.ctx.Log.Verbo(
			"failed to reissue tx",
			zap.Stringer("txID", b.Tx.ID()),
			zap.Stringer("blkID", blkID),
			zap.Error(err),
		)
	}

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) AtomicBlock(b *stateless.AtomicBlock) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting Atomic Block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)

	if err := r.Mempool.Add(b.Tx); err != nil {
		r.ctx.Log.Debug(
			"failed to reissue tx",
			zap.Stringer("txID", b.Tx.ID()),
			zap.Stringer("blkID", blkID),
			zap.Error(err),
		)
	}

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) StandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting Standard Block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)

	for _, tx := range b.Txs {
		if err := r.Mempool.Add(tx); err != nil {
			r.ctx.Log.Debug(
				"failed to reissue tx",
				zap.Stringer("txID", tx.ID()),
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
		}
	}

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) CommitBlock(b *stateless.CommitBlock) error {
	return r.rejectOptionBlock(b, true /* isCommit */)
}

func (r *rejector) AbortBlock(b *stateless.AbortBlock) error {
	return r.rejectOptionBlock(b, false /* isCommit */)
}

func (r *rejector) rejectOptionBlock(b stateless.Block, isCommit bool) error {
	blkID := b.ID()
	defer r.free(blkID)

	if isCommit {
		r.ctx.Log.Verbo(
			"rejecting Commit Block",
			zap.Stringer("blkID", blkID),
			zap.Uint64("height", b.Height()),
			zap.Stringer("parent", b.Parent()),
		)
	} else {
		r.ctx.Log.Verbo(
			"rejecting Abort Block",
			zap.Stringer("blkID", blkID),
			zap.Uint64("height", b.Height()),
			zap.Stringer("parent", b.Parent()),
		)
	}

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}
