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
// All errors returned by this struct are fatal and should result in the chain
// being shutdown.
type rejector struct {
	*backend
}

func (r *rejector) ProposalBlock(b *blocks.ProposalBlock) error {
	return r.rejectBlock(b, "proposal")
}

func (r *rejector) AtomicBlock(b *blocks.AtomicBlock) error {
	return r.rejectBlock(b, "atomic")
}

func (r *rejector) StandardBlock(b *blocks.StandardBlock) error {
	return r.rejectBlock(b, "standard")
}

func (r *rejector) CommitBlock(b *blocks.CommitBlock) error {
	return r.rejectBlock(b, "commit")
}

func (r *rejector) AbortBlock(b *blocks.AbortBlock) error {
	return r.rejectBlock(b, "abort")
}

func (r *rejector) rejectBlock(b blocks.Block, blockType string) error {
	blkID := b.ID()
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
	err := r.state.Commit()
	r.free(blkID)
	return err
}
