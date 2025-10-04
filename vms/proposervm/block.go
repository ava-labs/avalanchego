// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

const (
	// allowable block issuance in the future
	maxSkew = 10 * time.Second
)

var (
	errUnsignedChild            = errors.New("expected child to be signed")
	errUnexpectedBlockType      = errors.New("unexpected proposer block type")
	errInnerParentMismatch      = errors.New("inner parentID didn't match expected parent")
	errTimeNotMonotonic         = errors.New("time must monotonically increase")
	errPChainHeightNotMonotonic = errors.New("non monotonically increasing P-chain height")
	errPChainHeightNotReached   = errors.New("block P-chain height larger than current P-chain height")
	errTimeTooAdvanced          = errors.New("time is too far advanced")
	errProposerWindowNotStarted = errors.New("proposer window hasn't started")
	errUnexpectedProposer       = errors.New("unexpected proposer for current window")
	errProposerMismatch         = errors.New("proposer mismatch")
	errProposersNotActivated    = errors.New("proposers haven't been activated yet")
	errPChainHeightTooLow       = errors.New("block P-chain height is too low")
	errEpochNotZero             = errors.New("epoch must not be provided prior to granite")
)

type Block interface {
	snowman.Block

	getInnerBlk() snowman.Block

	// After a state sync, we may need to update last accepted block data
	// without propagating any changes to the innerVM.
	// acceptOuterBlk and acceptInnerBlk allow controlling acceptance of outer
	// and inner blocks.
	acceptOuterBlk() error
	acceptInnerBlk(context.Context) error

	verifyPreForkChild(ctx context.Context, child *preForkBlock) error
	verifyPostForkChild(ctx context.Context, child *postForkBlock) error
	verifyPostForkOption(ctx context.Context, child *postForkOption) error

	buildChild(context.Context) (Block, error)

	pChainHeight(context.Context) (uint64, error)
	pChainEpoch(context.Context) (block.Epoch, error)
	selectChildPChainHeight(context.Context) (uint64, error)
}

type PostForkBlock interface {
	Block

	getStatelessBlk() block.Block
	setInnerBlk(snowman.Block)
}

// field of postForkBlock and postForkOption
type postForkCommonComponents struct {
	vm       *VM
	innerBlk snowman.Block
}

// Return the inner block's height
func (p *postForkCommonComponents) Height() uint64 {
	return p.innerBlk.Height()
}

// makeEpoch returns a child block's epoch based on its parent.
func makeEpoch(
	upgrades upgrade.Config,
	parentPChainHeight uint64,
	parentEpoch block.Epoch,
	parentTimestamp time.Time,
	childTimestamp time.Time,
) block.Epoch {
	if !upgrades.IsGraniteActivated(childTimestamp) {
		return block.Epoch{}
	}

	if parentEpoch == (block.Epoch{}) {
		// If the parent was not assigned an epoch, then the child is the first
		// block of the initial epoch.
		return block.Epoch{
			PChainHeight: parentPChainHeight,
			Number:       1,
			StartTime:    parentTimestamp.Unix(),
		}
	}

	epochEndTime := time.Unix(parentEpoch.StartTime, 0).Add(upgrades.GraniteEpochDuration)
	if parentTimestamp.Before(epochEndTime) {
		// If the parent was issued before the end of its epoch, then it did not
		// seal the epoch.
		return parentEpoch
	}

	// The parent sealed the epoch. So, the child is the first block of the new
	// epoch.
	return block.Epoch{
		PChainHeight: parentPChainHeight,
		Number:       parentEpoch.Number + 1,
		StartTime:    parentTimestamp.Unix(),
	}
}

// Verify returns nil if:
// 1) [p]'s inner block is not an oracle block
// 2) [child]'s P-Chain height >= [parentPChainHeight]
// 3) [p]'s inner block is the parent of [c]'s inner block
// 4) [child]'s timestamp isn't before [p]'s timestamp
// 5) [child]'s timestamp is within the skew bound
// 6) [childPChainHeight] <= the current P-Chain height
// 7) [child]'s timestamp is within its proposer's window
// 8) [child] has a valid signature from its proposer
// 9) [child]'s inner block is valid
// 10) [child] has the expected epoch
func (p *postForkCommonComponents) Verify(
	ctx context.Context,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	parentEpoch block.Epoch,
	child *postForkBlock,
) error {
	if err := verifyIsNotOracleBlock(ctx, p.innerBlk); err != nil {
		return err
	}

	childPChainHeight := child.PChainHeight()
	if childPChainHeight < parentPChainHeight {
		return errPChainHeightNotMonotonic
	}

	expectedInnerParentID := p.innerBlk.ID()
	innerParentID := child.innerBlk.Parent()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	maxTimestamp := p.vm.Time().Add(maxSkew)
	if childTimestamp.After(maxTimestamp) {
		return errTimeTooAdvanced
	}

	childEpoch := child.PChainEpoch()
	if expected := makeEpoch(p.vm.Upgrades, parentPChainHeight, parentEpoch, parentTimestamp, childTimestamp); childEpoch != expected {
		return fmt.Errorf("epoch mismatch: epoch %v != expected %v", childEpoch, expected)
	}

	// If the node is currently syncing - we don't assume that the P-chain has
	// been synced up to this point yet.
	if p.vm.consensusState == snow.NormalOp {
		currentPChainHeight, err := p.vm.ctx.ValidatorState.GetCurrentHeight(ctx)
		if err != nil {
			p.vm.ctx.Log.Error("block verification failed",
				zap.String("reason", "failed to get current P-Chain height"),
				zap.Stringer("blkID", child.ID()),
				zap.Error(err),
			)
			return err
		}
		if childPChainHeight > currentPChainHeight {
			return fmt.Errorf("%w: %d > %d",
				errPChainHeightNotReached,
				childPChainHeight,
				currentPChainHeight,
			)
		}

		var shouldHaveProposer bool
		if p.vm.Upgrades.IsDurangoActivated(parentTimestamp) {
			shouldHaveProposer, err = p.verifyPostDurangoBlockDelay(ctx, parentTimestamp, parentPChainHeight, child)
		} else {
			shouldHaveProposer, err = p.verifyPreDurangoBlockDelay(ctx, parentTimestamp, parentPChainHeight, child)
		}
		if err != nil {
			return err
		}

		hasProposer := child.SignedBlock.Proposer() != ids.EmptyNodeID
		if shouldHaveProposer != hasProposer {
			return fmt.Errorf("%w: shouldHaveProposer (%v) != hasProposer (%v)", errProposerMismatch, shouldHaveProposer, hasProposer)
		}

		p.vm.ctx.Log.Debug("verified post-fork block",
			zap.Stringer("blkID", child.ID()),
			zap.Time("parentTimestamp", parentTimestamp),
			zap.Time("blockTimestamp", childTimestamp),
		)
	}

	var contextPChainHeight uint64
	switch {
	case p.vm.Upgrades.IsGraniteActivated(childTimestamp):
		contextPChainHeight = childEpoch.PChainHeight
	case p.vm.Upgrades.IsEtnaActivated(childTimestamp):
		contextPChainHeight = childPChainHeight
	default:
		contextPChainHeight = parentPChainHeight
	}

	return p.vm.verifyAndRecordInnerBlk(
		ctx,
		&smblock.Context{
			PChainHeight: contextPChainHeight,
		},
		child,
	)
}

// Return the child (a *postForkBlock) of this block
func (p *postForkCommonComponents) buildChild(
	ctx context.Context,
	parentID ids.ID,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	parentEpoch block.Epoch,
) (Block, error) {
	// Child's timestamp is the later of now and this block's timestamp
	newTimestamp := p.vm.Time().Truncate(time.Second)
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
	}

	// The child's P-Chain height is proposed as the optimal P-Chain height that
	// is at least the parent's P-Chain height
	pChainHeight, err := p.vm.selectChildPChainHeight(ctx, parentPChainHeight)
	if err != nil {
		p.vm.ctx.Log.Error("unexpected build block failure",
			zap.String("reason", "failed to calculate optimal P-chain height"),
			zap.Stringer("parentID", parentID),
			zap.Error(err),
		)
		return nil, err
	}

	var shouldBuildSignedBlock bool
	if p.vm.Upgrades.IsDurangoActivated(parentTimestamp) {
		shouldBuildSignedBlock, err = p.shouldBuildSignedBlockPostDurango(
			ctx,
			parentID,
			parentTimestamp,
			parentPChainHeight,
			newTimestamp,
		)
	} else {
		shouldBuildSignedBlock, err = p.shouldBuildSignedBlockPreDurango(
			ctx,
			parentID,
			parentTimestamp,
			parentPChainHeight,
			newTimestamp,
		)
	}
	if err != nil {
		return nil, err
	}

	epoch := makeEpoch(p.vm.Upgrades, parentPChainHeight, parentEpoch, parentTimestamp, newTimestamp)

	var contextPChainHeight uint64
	switch {
	case p.vm.Upgrades.IsGraniteActivated(newTimestamp):
		contextPChainHeight = epoch.PChainHeight
	case p.vm.Upgrades.IsEtnaActivated(newTimestamp):
		contextPChainHeight = pChainHeight
	default:
		contextPChainHeight = parentPChainHeight
	}

	var innerBlock snowman.Block
	if p.vm.blockBuilderVM != nil {
		innerBlock, err = p.vm.blockBuilderVM.BuildBlockWithContext(ctx, &smblock.Context{
			PChainHeight: contextPChainHeight,
		})
	} else {
		innerBlock, err = p.vm.ChainVM.BuildBlock(ctx)
	}
	if err != nil {
		return nil, err
	}

	// Build the child
	var statelessChild block.SignedBlock
	if shouldBuildSignedBlock {
		statelessChild, err = block.Build(
			parentID,
			newTimestamp,
			pChainHeight,
			epoch,
			p.vm.StakingCertLeaf,
			innerBlock.Bytes(),
			p.vm.ctx.ChainID,
			p.vm.StakingLeafSigner,
		)
	} else {
		statelessChild, err = block.BuildUnsigned(
			parentID,
			newTimestamp,
			pChainHeight,
			epoch,
			innerBlock.Bytes(),
		)
	}
	if err != nil {
		p.vm.ctx.Log.Error("unexpected build block failure",
			zap.String("reason", "failed to generate proposervm block header"),
			zap.Stringer("parentID", parentID),
			zap.Stringer("blkID", innerBlock.ID()),
			zap.Error(err),
		)
		return nil, err
	}

	child := &postForkBlock{
		SignedBlock: statelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       p.vm,
			innerBlk: innerBlock,
		},
	}

	p.vm.ctx.Log.Info("built block",
		zap.Stringer("blkID", child.ID()),
		zap.Stringer("innerBlkID", innerBlock.ID()),
		zap.Uint64("height", child.Height()),
		zap.Uint64("pChainHeight", pChainHeight),
		zap.Time("parentTimestamp", parentTimestamp),
		zap.Time("blockTimestamp", newTimestamp),
		zap.Reflect("epoch", epoch),
	)
	return child, nil
}

func (p *postForkCommonComponents) getInnerBlk() snowman.Block {
	return p.innerBlk
}

func (p *postForkCommonComponents) setInnerBlk(innerBlk snowman.Block) {
	p.innerBlk = innerBlk
}

func verifyIsOracleBlock(ctx context.Context, b snowman.Block) error {
	oracle, ok := b.(snowman.OracleBlock)
	if !ok {
		return fmt.Errorf(
			"%w: expected block %s to be a snowman.OracleBlock but it's a %T",
			errUnexpectedBlockType, b.ID(), b,
		)
	}
	_, err := oracle.Options(ctx)
	return err
}

func verifyIsNotOracleBlock(ctx context.Context, b snowman.Block) error {
	oracle, ok := b.(snowman.OracleBlock)
	if !ok {
		return nil
	}
	_, err := oracle.Options(ctx)
	switch err {
	case nil:
		return fmt.Errorf(
			"%w: expected block %s not to be an oracle block but it's a %T",
			errUnexpectedBlockType, b.ID(), b,
		)
	case snowman.ErrNotOracle:
		return nil
	default:
		return err
	}
}

func (p *postForkCommonComponents) verifyPreDurangoBlockDelay(
	ctx context.Context,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	blk *postForkBlock,
) (bool, error) {
	var (
		blkTimestamp = blk.Timestamp()
		childHeight  = blk.Height()
		proposerID   = blk.Proposer()
	)
	minDelay, err := p.vm.Windower.Delay(
		ctx,
		childHeight,
		parentPChainHeight,
		proposerID,
		proposer.MaxVerifyWindows,
	)
	if err != nil {
		p.vm.ctx.Log.Error("unexpected block verification failure",
			zap.String("reason", "failed to calculate required timestamp delay"),
			zap.Stringer("blkID", blk.ID()),
			zap.Error(err),
		)
		return false, err
	}

	delay := blkTimestamp.Sub(parentTimestamp)
	if delay < minDelay {
		return false, fmt.Errorf("%w: delay %s < minDelay %s", errProposerWindowNotStarted, delay, minDelay)
	}

	return delay < proposer.MaxVerifyDelay, nil
}

func (p *postForkCommonComponents) verifyPostDurangoBlockDelay(
	ctx context.Context,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	blk *postForkBlock,
) (bool, error) {
	var (
		blkTimestamp = blk.Timestamp()
		blkHeight    = blk.Height()
		currentSlot  = proposer.TimeToSlot(parentTimestamp, blkTimestamp)
		proposerID   = blk.Proposer()
	)
	// populate the slot for the block.
	blk.slot = &currentSlot

	// find the expected proposer
	expectedProposerID, err := p.vm.Windower.ExpectedProposer(
		ctx,
		blkHeight,
		parentPChainHeight,
		currentSlot,
	)
	switch {
	case errors.Is(err, proposer.ErrAnyoneCanPropose):
		return false, nil // block should be unsigned
	case err != nil:
		p.vm.ctx.Log.Error("unexpected block verification failure",
			zap.String("reason", "failed to calculate expected proposer"),
			zap.Stringer("blkID", blk.ID()),
			zap.Error(err),
		)
		return false, err
	case expectedProposerID == proposerID:
		return true, nil // block should be signed
	default:
		return false, fmt.Errorf("%w: slot %d expects %s", errUnexpectedProposer, currentSlot, expectedProposerID)
	}
}

func (p *postForkCommonComponents) shouldBuildSignedBlockPostDurango(
	ctx context.Context,
	parentID ids.ID,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	newTimestamp time.Time,
) (bool, error) {
	parentHeight := p.innerBlk.Height()
	currentSlot := proposer.TimeToSlot(parentTimestamp, newTimestamp)
	expectedProposerID, err := p.vm.Windower.ExpectedProposer(
		ctx,
		parentHeight+1,
		parentPChainHeight,
		currentSlot,
	)
	switch {
	case errors.Is(err, proposer.ErrAnyoneCanPropose):
		return false, nil // build an unsigned block
	case err != nil:
		p.vm.ctx.Log.Error("unexpected build block failure",
			zap.String("reason", "failed to calculate expected proposer"),
			zap.Stringer("parentID", parentID),
			zap.Error(err),
		)
		return false, err
	case expectedProposerID == p.vm.ctx.NodeID:
		return true, nil // build a signed block
	}

	// It's not our turn to propose a block yet. This is likely caused by having
	// previously notified the consensus engine to attempt to build a block on
	// top of a block that is no longer the preferred block.
	p.vm.ctx.Log.Debug("build block dropped",
		zap.Time("parentTimestamp", parentTimestamp),
		zap.Time("blockTimestamp", newTimestamp),
		zap.Uint64("slot", currentSlot),
		zap.Stringer("expectedProposer", expectedProposerID),
	)
	return false, fmt.Errorf("%w: slot %d expects %s", errUnexpectedProposer, currentSlot, expectedProposerID)
}

func (p *postForkCommonComponents) shouldBuildSignedBlockPreDurango(
	ctx context.Context,
	parentID ids.ID,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	newTimestamp time.Time,
) (bool, error) {
	delay := newTimestamp.Sub(parentTimestamp)
	if delay >= proposer.MaxBuildDelay {
		return false, nil // time for any node to build an unsigned block
	}

	parentHeight := p.innerBlk.Height()
	proposerID := p.vm.ctx.NodeID
	minDelay, err := p.vm.Windower.Delay(ctx, parentHeight+1, parentPChainHeight, proposerID, proposer.MaxBuildWindows)
	if err != nil {
		p.vm.ctx.Log.Error("unexpected build block failure",
			zap.String("reason", "failed to calculate required timestamp delay"),
			zap.Stringer("parentID", parentID),
			zap.Error(err),
		)
		return false, err
	}

	if delay >= minDelay {
		// it's time for this node to propose a block. It'll be signed or
		// unsigned depending on the delay
		return delay < proposer.MaxVerifyDelay, nil
	}

	// It's not our turn to propose a block yet. This is likely caused by having
	// previously notified the consensus engine to attempt to build a block on
	// top of a block that is no longer the preferred block.
	p.vm.ctx.Log.Debug("build block dropped",
		zap.Time("parentTimestamp", parentTimestamp),
		zap.Duration("minDelay", minDelay),
		zap.Time("blockTimestamp", newTimestamp),
	)
	return false, fmt.Errorf("%w: delay %s < minDelay %s", errProposerWindowNotStarted, delay, minDelay)
}
