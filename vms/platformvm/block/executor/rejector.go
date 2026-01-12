// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

var _ block.Visitor = (*rejector)(nil)

// rejector handles the logic for rejecting a block.
// All errors returned by this struct are fatal and should result in the chain
// being shutdown.
type rejector struct {
	*backend
	addTxsToMempool bool
}

func (r *rejector) BanffAbortBlock(b *block.BanffAbortBlock) error {
	return r.rejectBlock(b, "banff abort")
}

func (r *rejector) BanffCommitBlock(b *block.BanffCommitBlock) error {
	return r.rejectBlock(b, "banff commit")
}

func (r *rejector) BanffProposalBlock(b *block.BanffProposalBlock) error {
	return r.rejectBlock(b, "banff proposal")
}

func (r *rejector) BanffStandardBlock(b *block.BanffStandardBlock) error {
	return r.rejectBlock(b, "banff standard")
}

func (r *rejector) ApricotAbortBlock(b *block.ApricotAbortBlock) error {
	return r.rejectBlock(b, "apricot abort")
}

func (r *rejector) ApricotCommitBlock(b *block.ApricotCommitBlock) error {
	return r.rejectBlock(b, "apricot commit")
}

func (r *rejector) ApricotProposalBlock(b *block.ApricotProposalBlock) error {
	return r.rejectBlock(b, "apricot proposal")
}

func (r *rejector) ApricotStandardBlock(b *block.ApricotStandardBlock) error {
	return r.rejectBlock(b, "apricot standard")
}

func (r *rejector) ApricotAtomicBlock(b *block.ApricotAtomicBlock) error {
	return r.rejectBlock(b, "apricot atomic")
}

func (r *rejector) rejectBlock(b block.Block, blockType string) error {
	blkID := b.ID()
	defer r.free(blkID)

	r.ctx.Log.Verbo(
		"rejecting block",
		zap.String("blockType", blockType),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
	)

	if !r.addTxsToMempool {
		return nil
	}

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

	if r.Mempool.Len() == 0 {
		return nil
	}

	return nil
}
