// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ PostForkBlock = (*postForkOption)(nil)

// The parent of a *postForkOption must be a *postForkBlock.
type postForkOption struct {
	block.Block
	postForkCommonComponents

	timestamp time.Time
}

func (b *postForkOption) Timestamp() time.Time {
	if b.Height() <= b.vm.lastAcceptedHeight {
		return b.vm.lastAcceptedTime
	}
	return b.timestamp
}

func (b *postForkOption) Accept(ctx context.Context) error {
	if err := b.acceptOuterBlk(); err != nil {
		return err
	}
	return b.acceptInnerBlk(ctx)
}

func (b *postForkOption) acceptOuterBlk() error {
	return b.vm.acceptPostForkBlock(b)
}

func (b *postForkOption) acceptInnerBlk(ctx context.Context) error {
	// mark the inner block as accepted and all conflicting inner blocks as
	// rejected
	return b.vm.Tree.Accept(ctx, b.innerBlk)
}

func (b *postForkOption) Reject(context.Context) error {
	// we do not reject the inner block here because that block may be contained
	// in the proposer block that causing this block to be rejected.

	delete(b.vm.verifiedBlocks, b.ID())
	return nil
}

func (b *postForkOption) Parent() ids.ID {
	return b.ParentID()
}

// If Verify returns nil, Accept or Reject is eventually called on [b] and
// [b.innerBlk].
func (b *postForkOption) Verify(ctx context.Context) error {
	parent, err := b.vm.getBlock(ctx, b.ParentID())
	if err != nil {
		return err
	}
	b.timestamp = parent.Timestamp()
	return parent.verifyPostForkOption(ctx, b)
}

func (*postForkOption) verifyPreForkChild(context.Context, *preForkBlock) error {
	// A *preForkBlock's parent must be a *preForkBlock
	return errUnsignedChild
}

func (b *postForkOption) verifyPostForkChild(ctx context.Context, child *postForkBlock) error {
	parentTimestamp := b.Timestamp()
	parentPChainHeight, err := b.pChainHeight(ctx)
	if err != nil {
		return err
	}
	parentEpoch, err := b.pChainEpoch(ctx)
	if err != nil {
		return err
	}

	return b.postForkCommonComponents.Verify(
		ctx,
		parentTimestamp,
		parentPChainHeight,
		parentEpoch,
		child,
	)
}

func (*postForkOption) verifyPostForkOption(context.Context, *postForkOption) error {
	// A *postForkOption's parent can't be a *postForkOption
	return errUnexpectedBlockType
}

func (b *postForkOption) buildChild(ctx context.Context) (Block, error) {
	parentID := b.ID()
	parentPChainHeight, err := b.pChainHeight(ctx)
	if err != nil {
		b.vm.ctx.Log.Error("unexpected build block failure",
			zap.String("reason", "failed to fetch parent's P-chain height"),
			zap.Stringer("parentID", parentID),
			zap.Error(err),
		)
		return nil, err
	}
	parentEpoch, err := b.pChainEpoch(ctx)
	if err != nil {
		b.vm.ctx.Log.Error("unexpected build block failure",
			zap.String("reason", "failed to fetch parent's epoch"),
			zap.Stringer("parentID", parentID),
			zap.Error(err),
		)
		return nil, err
	}

	return b.postForkCommonComponents.buildChild(
		ctx,
		parentID,
		b.Timestamp(),
		parentPChainHeight,
		parentEpoch,
	)
}

// This block's P-Chain height is its parent's P-Chain height
func (b *postForkOption) pChainHeight(ctx context.Context) (uint64, error) {
	parent, err := b.vm.getBlock(ctx, b.ParentID())
	if err != nil {
		return 0, err
	}
	return parent.pChainHeight(ctx)
}

func (b *postForkOption) pChainEpoch(ctx context.Context) (block.Epoch, error) {
	parent, err := b.vm.getBlock(ctx, b.ParentID())
	if err != nil {
		return block.Epoch{}, err
	}
	return parent.pChainEpoch(ctx)
}

func (b *postForkOption) selectChildPChainHeight(ctx context.Context) (uint64, error) {
	pChainHeight, err := b.pChainHeight(ctx)
	if err != nil {
		return 0, err
	}

	return b.vm.selectChildPChainHeight(ctx, pChainHeight)
}

func (b *postForkOption) getStatelessBlk() block.Block {
	return b.Block
}
