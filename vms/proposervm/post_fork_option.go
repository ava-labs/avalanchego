// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
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
	if b.Status() == choices.Accepted {
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
	// Update in-memory references
	b.status = choices.Accepted
	b.vm.lastAcceptedHeight = b.Height()

	blkID := b.ID()
	delete(b.vm.verifiedBlocks, blkID)

	// Persist this block, its height index, and its status
	if err := b.vm.State.SetLastAccepted(blkID); err != nil {
		return err
	}
	return b.vm.storePostForkBlock(b)
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
	b.status = choices.Rejected
	return nil
}

func (b *postForkOption) Status() choices.Status {
	if b.status == choices.Accepted && b.Height() > b.vm.lastAcceptedHeight {
		return choices.Processing
	}
	return b.status
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
	return b.postForkCommonComponents.Verify(
		ctx,
		parentTimestamp,
		parentPChainHeight,
		child,
	)
}

func (*postForkOption) verifyPostForkOption(context.Context, *postForkOption) error {
	// A *postForkOption's parent can't be a *postForkOption
	return errUnexpectedBlockType
}

func (b *postForkOption) buildChild(ctx context.Context) (Block, error) {
	parentPChainHeight, err := b.pChainHeight(ctx)
	if err != nil {
		return nil, err
	}
	return b.postForkCommonComponents.buildChild(
		ctx,
		b.ID(),
		b.Timestamp(),
		parentPChainHeight,
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

func (b *postForkOption) setStatus(status choices.Status) {
	b.status = status
}

func (b *postForkOption) getStatelessBlk() block.Block {
	return b.Block
}
