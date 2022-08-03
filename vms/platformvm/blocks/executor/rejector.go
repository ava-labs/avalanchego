// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
)

var _ blocks.Visitor = &rejector{}

// rejector handles the logic for rejecting a block.
type rejector struct {
	*backend
}

func (r *rejector) BlueberryProposalBlock(b *blocks.BlueberryProposalBlock) error {
	return r.visitProposalBlock(b)
}

func (r *rejector) ApricotProposalBlock(b *blocks.ApricotProposalBlock) error {
	return r.visitProposalBlock(b)
}

func (r *rejector) visitProposalBlock(b blocks.Block) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting block",
		zap.String("blockType", "proposal"),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)

	tx := b.Txs()[0]
	if err := r.Mempool.Add(tx); err != nil {
		r.ctx.Log.Verbo(
			"failed to reissue tx",
			zap.Stringer("txID", tx.ID()),
			zap.Stringer("blkID", blkID),
			zap.Error(err),
		)
	}

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}

func (r *rejector) AtomicBlock(b *blocks.AtomicBlock) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting block",
		zap.String("blockType", "atomic"),
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

func (r *rejector) BlueberryStandardBlock(b *blocks.BlueberryStandardBlock) error {
	return r.visitStandardBlock(b)
}

func (r *rejector) ApricotStandardBlock(b *blocks.ApricotStandardBlock) error {
	return r.visitStandardBlock(b)
}

func (r *rejector) visitStandardBlock(b blocks.Block) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting block",
		zap.String("blockType", "standard"),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)

	txs := b.Txs()
	for _, tx := range txs {
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

func (r *rejector) CommitBlock(b *blocks.CommitBlock) error {
	r.ctx.Log.Verbo(
		"rejecting block",
		zap.String("blockType", "commit"),
		zap.Stringer("blkID", b.ID()),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)
	return r.rejectOptionBlock(b)
}

func (r *rejector) AbortBlock(b *blocks.AbortBlock) error {
	r.ctx.Log.Verbo(
		"rejecting block",
		zap.String("blockType", "abort"),
		zap.Stringer("blkID", b.ID()),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)
	return r.rejectOptionBlock(b)
}

func (r *rejector) rejectOptionBlock(b blocks.Block) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.stateVersions.DeleteState(blkID)
	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}
