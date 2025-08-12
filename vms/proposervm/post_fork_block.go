// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ PostForkBlock = (*postForkBlock)(nil)

type postForkBlock struct {
	block.SignedBlock
	postForkCommonComponents

	// slot of the proposer that produced this block.
	// It is populated in verifyPostDurangoBlockDelay.
	// It is used to report metrics during Accept.
	slot *uint64
}

// Accept:
// 1) Sets this blocks status to Accepted.
// 2) Persists this block in storage
// 3) Calls Reject() on siblings of this block and their descendants.
func (b *postForkBlock) Accept(ctx context.Context) error {
	if err := b.acceptOuterBlk(); err != nil {
		return err
	}
	if err := b.acceptInnerBlk(ctx); err != nil {
		return err
	}
	if b.slot != nil {
		b.vm.acceptedBlocksSlotHistogram.Observe(float64(*b.slot))
	}
	b.updateLastAcceptedTimestampMetric(outerBlockTypeMetricLabel, b.Timestamp())
	b.updateLastAcceptedTimestampMetric(innerBlockTypeMetricLabel, b.innerBlk.Timestamp())
	return nil
}

const (
	innerBlockTypeMetricLabel = "inner"
	outerBlockTypeMetricLabel = "proposervm"
)

func (b *postForkBlock) updateLastAcceptedTimestampMetric(blockTypeLabel string, t time.Time) {
	g := b.vm.lastAcceptedTimestampGaugeVec.WithLabelValues(blockTypeLabel)
	g.Set(float64(t.Unix()))
}

func (b *postForkBlock) acceptOuterBlk() error {
	// Update in-memory references
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
	delete(b.vm.verifiedBlocks, b.ID())
	return nil
}

// Return this block's parent, or a *missing.Block if
// we don't have the parent.
func (b *postForkBlock) Parent() ids.ID {
	return b.ParentID()
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
			},
		}
	}
	return outerOptions, nil
}

// A post-fork block can never have a pre-fork child
func (*postForkBlock) verifyPreForkChild(context.Context, *preForkBlock) error {
	return errUnsignedChild
}

func (b *postForkBlock) verifyPostForkChild(ctx context.Context, child *postForkBlock) error {
	parentTimestamp := b.Timestamp()
	parentPChainHeight := b.PChainHeight()
	return b.postForkCommonComponents.Verify(
		ctx,
		parentTimestamp,
		parentPChainHeight,
		child,
	)
}

func (b *postForkBlock) verifyPostForkOption(ctx context.Context, child *postForkOption) error {
	if err := verifyIsOracleBlock(ctx, b.innerBlk); err != nil {
		return err
	}

	// Make sure [b]'s inner block is the parent of [child]'s inner block
	expectedInnerParentID := b.innerBlk.ID()
	innerParentID := child.innerBlk.Parent()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

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

func (b *postForkBlock) pChainEpochHeight(context.Context) (uint64, error) {
	return b.PChainEpochHeight(), nil
}

func (b *postForkBlock) epochNumber(context.Context) (uint64, error) {
	return b.EpochNumber(), nil
}

func (b *postForkBlock) epochStartTime(context.Context) (time.Time, error) {
	return b.EpochStartTime(), nil
}

func (b *postForkBlock) getStatelessBlk() block.Block {
	return b.SignedBlock
}
