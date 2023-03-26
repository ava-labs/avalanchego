// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
)

var _ blocks.Visitor = (*rejector)(nil)

// rejector handles the logic for rejecting a block.
// All errors returned by this struct are fatal and should result in the chain
// being shutdown.
type rejector struct {
	*backend
}

func (r *rejector) BanffAbortBlock(b *blocks.BanffAbortBlock) error {
	return r.rejectBlock(b, "banff abort")
}

func (r *rejector) BanffCommitBlock(b *blocks.BanffCommitBlock) error {
	return r.rejectBlock(b, "banff commit")
}

func (r *rejector) BanffProposalBlock(b *blocks.BanffProposalBlock) error {
	return r.rejectBlock(b, "banff proposal")
}

func (r *rejector) BanffStandardBlock(b *blocks.BanffStandardBlock) error {
	return r.rejectBlock(b, "banff standard")
}

func (r *rejector) ApricotAbortBlock(b *blocks.ApricotAbortBlock) error {
	return r.rejectBlock(b, "apricot abort")
}

func (r *rejector) ApricotCommitBlock(b *blocks.ApricotCommitBlock) error {
	return r.rejectBlock(b, "apricot commit")
}

func (r *rejector) ApricotProposalBlock(b *blocks.ApricotProposalBlock) error {
	return r.rejectBlock(b, "apricot proposal")
}

func (r *rejector) ApricotStandardBlock(b *blocks.ApricotStandardBlock) error {
	return r.rejectBlock(b, "apricot standard")
}

func (r *rejector) ApricotAtomicBlock(b *blocks.ApricotAtomicBlock) error {
	return r.rejectBlock(b, "apricot atomic")
}

func (r *rejector) rejectBlock(b blocks.Block, blockType string) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting block",
		zap.String("blockType", blockType),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
	)

	for _, tx := range b.Txs() {
		if err := r.Mempool.Add(tx); err != nil {
			r.ctx.Log.Debug(
				"failed to reissue tx",
				zap.Stringer("txID", tx.ID()),
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
		}
	}

	r.state.AddStatelessBlock(b, choices.Rejected)
	return r.state.Commit()
}
