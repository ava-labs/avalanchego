// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ PostForkBlock = (*postForkBlock)(nil)

type postForkBlock struct {
	block.SignedBlock
	postForkCommonComponents
}

// Accept:
// 1) Sets this blocks status to Accepted.
// 2) Persists this block in storage
// 3) Calls Reject() on siblings of this block and their descendants.
func (b *postForkBlock) Accept(ctx context.Context) error {
	if err := b.acceptOuterBlk(); err != nil {
		return err
	}
	return b.acceptInnerBlk(ctx)
}

func (b *postForkBlock) acceptOuterBlk() error {
	// Update in-memory references
	b.status = choices.Accepted
	b.vm.lastAcceptedTime = b.Timestamp()

	return b.vm.acceptPostForkBlock(b)
}

func (b *postForkBlock) acceptInnerBlk(ctx context.Context) error {
	// mark the inner block as accepted and all conflicting inner blocks as
	// rejected
	return b.vm.Tree.Accept(ctx, b.innerBlk)
}

func (b *postForkBlock) Reject(context.Context) error {
	// We do not reject the inner block here because it may be accepted later
	blkID := b.ID()
	delete(b.vm.verifiedProposerBlocks, blkID)
	delete(b.vm.verifiedBlocks, blkID)
	b.status = choices.Rejected
	return nil
}

func (b *postForkBlock) Status() choices.Status {
	if b.status == choices.Accepted && b.Height() > b.vm.lastAcceptedHeight {
		return choices.Processing
	}
	return b.status
}

// Return this block's parent, or a *missing.Block if
// we don't have the parent.
func (b *postForkBlock) Parent() ids.ID {
	return b.ParentID()
}

func (b *postForkBlock) VerifyProposer(ctx context.Context) error {
	parent, err := b.vm.getBlock(ctx, b.ParentID())
	if err != nil {
		return err
	}
	return parent.verifyProposerPostForkChild(ctx, b)
}

// If Verify() returns nil, Accept() or Reject() will eventually be called on
// [b] and [b.innerBlk]
func (b *postForkBlock) Verify(ctx context.Context) error {
	parent, err := b.vm.getBlock(ctx, b.ParentID())
	if err != nil {
		return err
	}
	return parent.verifyPostForkChild(ctx, b)
}

// Return the two options for the block that follows [b]
func (b *postForkBlock) Options(ctx context.Context) ([2]snowman.Block, error) {
	innerOracleBlk, ok := b.innerBlk.(snowman.OracleBlock)
	if !ok {
		// [b]'s innerBlk isn't an oracle block
		return [2]snowman.Block{}, snowman.ErrNotOracle
	}

	// The inner block's child options
	innerOptions, err := innerOracleBlk.Options(ctx)
	if err != nil {
		return [2]snowman.Block{}, err
	}

	parentID := b.ID()
	outerOptions := [2]snowman.Block{}
	for i, innerOption := range innerOptions {
		// Wrap the inner block's child option
		statelessOuterOption, err := block.BuildOption(
			parentID,
			innerOption.Bytes(),
		)
		if err != nil {
			return [2]snowman.Block{}, err
		}

		outerOptions[i] = &postForkOption{
			Block: statelessOuterOption,
			postForkCommonComponents: postForkCommonComponents{
				vm:       b.vm,
				innerBlk: innerOption,
				status:   innerOption.Status(),
			},
		}
	}
	return outerOptions, nil
}

// A post-fork block can never have a pre-fork child
func (*postForkBlock) verifyPreForkChild(context.Context, *preForkBlock) error {
	return errUnsignedChild
}

func (b *postForkBlock) verifyProposerPostForkChild(ctx context.Context, child *postForkBlock) error {
	err := b.postForkCommonComponents.Verify(
		ctx,
		b.Timestamp(),
		b.PChainHeight(),
		child,
	)
	if err != nil {
		return err
	}

	childID := child.ID()
	child.vm.verifiedProposerBlocks[childID] = child
	return nil
}

func (b *postForkBlock) verifyPostForkChild(ctx context.Context, child *postForkBlock) error {
	return child.vm.verifyAndRecordInnerBlk(
		ctx,
		&smblock.Context{
			PChainHeight: b.PChainHeight(),
		},
		child,
	)
}

func (b *postForkBlock) verifyProposerPostForkOption(ctx context.Context, child *postForkOption) error {
	if err := verifyIsOracleBlock(ctx, b.innerBlk); err != nil {
		return err
	}

	// Make sure [b]'s inner block is the parent of [child]'s inner block
	expectedInnerParentID := b.innerBlk.ID()
	innerParentID := child.innerBlk.Parent()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	childID := child.ID()
	child.vm.verifiedProposerBlocks[childID] = child
	return nil
}

func (*postForkBlock) verifyPostForkOption(ctx context.Context, child *postForkOption) error {
	return child.vm.verifyAndRecordInnerBlk(ctx, nil, child)
}

// Return the child (a *postForkBlock) of this block
func (b *postForkBlock) buildChild(ctx context.Context) (Block, error) {
	return b.postForkCommonComponents.buildChild(
		ctx,
		b.ID(),
		b.Timestamp(),
		b.PChainHeight(),
	)
}

func (b *postForkBlock) pChainHeight(context.Context) (uint64, error) {
	return b.PChainHeight(), nil
}

func (b *postForkBlock) setStatus(status choices.Status) {
	b.status = status
}

func (b *postForkBlock) getStatelessBlk() block.Block {
	return b.SignedBlock
}
