// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
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
	errProposersNotActivated    = errors.New("proposers haven't been activated yet")
	errPChainHeightTooLow       = errors.New("block P-chain height is too low")
)

type Block interface {
	snowman.Block

	getInnerBlk() snowman.Block

	// After a state sync, we may need to update last accepted block data
	// without propagating any changes to the innerVM.
	// acceptOuterBlk and acceptInnerBlk allow controlling acceptance of outer
	// and inner blocks.
	acceptOuterBlk() error
	acceptInnerBlk() error

	verifyPreForkChild(child *preForkBlock) error
	verifyPostForkChild(child *postForkBlock) error
	verifyPostForkOption(child *postForkOption) error

	buildChild() (Block, error)

	pChainHeight() (uint64, error)
}

type PostForkBlock interface {
	Block

	setStatus(choices.Status)
	getStatelessBlk() block.Block
	setInnerBlk(snowman.Block)
}

// field of postForkBlock and postForkOption
type postForkCommonComponents struct {
	vm       *VM
	innerBlk snowman.Block
	status   choices.Status
}

// Return the inner block's height
func (p *postForkCommonComponents) Height() uint64 {
	return p.innerBlk.Height()
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
func (p *postForkCommonComponents) Verify(parentTimestamp time.Time, parentPChainHeight uint64, child *postForkBlock) error {
	if err := verifyIsNotOracleBlock(p.innerBlk); err != nil {
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

	// If the node is currently syncing - we don't assume that the P-chain has
	// been synced up to this point yet.
	if p.vm.consensusState == snow.NormalOp {
		childID := child.ID()
		currentPChainHeight, err := p.vm.ctx.ValidatorState.GetCurrentHeight()
		if err != nil {
			p.vm.ctx.Log.Error("block verification failed",
				zap.String("reason", "failed to get current P-Chain height"),
				zap.Stringer("blkID", childID),
				zap.Error(err),
			)
			return err
		}
		if childPChainHeight > currentPChainHeight {
			return errPChainHeightNotReached
		}

		childHeight := child.Height()
		proposerID := child.Proposer()
		p.vm.Retriever.SetChainHeight(childHeight)
		p.vm.Retriever.SetPChainHeight(parentPChainHeight)
		minDelay, err := p.vm.Windower.Delay(p.vm.ctx.NodeID)
		if err != nil {
			return err
		}

		delay := childTimestamp.Sub(parentTimestamp)
		if delay < minDelay {
			return errProposerWindowNotStarted
		}

		// Verify the signature of the node
		shouldHaveProposer := delay < proposer.MaxDelay
		if err := child.SignedBlock.Verify(shouldHaveProposer, p.vm.ctx.ChainID); err != nil {
			return err
		}

		p.vm.ctx.Log.Debug("verified post-fork block",
			zap.Stringer("blkID", childID),
			zap.Time("parentTimestamp", parentTimestamp),
			zap.Duration("minDelay", minDelay),
			zap.Time("blockTimestamp", childTimestamp),
		)
	}

	return p.vm.verifyAndRecordInnerBlk(child)
}

// Return the child (a *postForkBlock) of this block
func (p *postForkCommonComponents) buildChild(
	parentID ids.ID,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
) (Block, error) {
	// Child's timestamp is the later of now and this block's timestamp
	newTimestamp := p.vm.Time().Truncate(time.Second)
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
	}

	// The child's P-Chain height is proposed as the optimal P-Chain height that
	// is at least the parent's P-Chain height
	pChainHeight, err := p.vm.optimalPChainHeight(parentPChainHeight)
	if err != nil {
		return nil, err
	}

	delay := newTimestamp.Sub(parentTimestamp)
	if delay < proposer.MaxDelay {
		parentHeight := p.innerBlk.Height()
		proposerID := p.vm.ctx.NodeID
		p.vm.Retriever.SetChainHeight(parentHeight+1)
		p.vm.Retriever.SetPChainHeight(parentPChainHeight)
		minDelay, err := p.vm.Windower.Delay(p.vm.ctx.NodeID)
		if err != nil {
			return nil, err
		}

		if delay < minDelay {
			// It's not our turn to propose a block yet. This is likely caused
			// by having previously notified the consensus engine to attempt to
			// build a block on top of a block that is no longer the preferred
			// block.
			p.vm.ctx.Log.Debug("build block dropped",
				zap.Time("parentTimestamp", parentTimestamp),
				zap.Duration("minDelay", minDelay),
				zap.Time("blockTimestamp", newTimestamp),
			)

			// In case the inner VM only issued one pendingTxs message, we
			// should attempt to re-handle that once it is our turn to build the
			// block.
			p.vm.notifyInnerBlockReady()
			return nil, errProposerWindowNotStarted
		}
	}

	innerBlock, err := p.vm.ChainVM.BuildBlock()
	if err != nil {
		return nil, err
	}

	banffActivated := newTimestamp.After(p.vm.activationTimeBanff)

	// Build the child
	var statelessChild block.SignedBlock
	if delay >= proposer.MaxDelay {
		if banffActivated {
			statelessChild, err = block.BuildUnsignedBanff(
				parentID,
				newTimestamp,
				pChainHeight,
				innerBlock.Bytes(),
			)
		} else {
			statelessChild, err = block.BuildUnsignedApricot(
				parentID,
				newTimestamp,
				pChainHeight,
				innerBlock.Bytes(),
			)
		}
		if err != nil {
			return nil, err
		}
	} else {
		if banffActivated {
			statelessChild, err = block.BuildBanff(
				parentID,
				newTimestamp,
				pChainHeight,
				p.vm.ctx.StakingCertLeaf,
				innerBlock.Bytes(),
				p.vm.ctx.ChainID,
				p.vm.ctx.StakingLeafSigner,
			)
		} else {
			statelessChild, err = block.BuildApricot(
				parentID,
				newTimestamp,
				pChainHeight,
				p.vm.ctx.StakingCertLeaf,
				innerBlock.Bytes(),
				p.vm.ctx.ChainID,
				p.vm.ctx.StakingLeafSigner,
			)
		}
		if err != nil {
			return nil, err
		}
	}

	child := &postForkBlock{
		SignedBlock: statelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       p.vm,
			innerBlk: innerBlock,
			status:   choices.Processing,
		},
	}

	p.vm.ctx.Log.Info("built block",
		zap.Stringer("blkID", child.ID()),
		zap.Stringer("innerBlkID", innerBlock.ID()),
		zap.Uint64("height", child.Height()),
		zap.Time("parentTimestamp", parentTimestamp),
		zap.Time("blockTimestamp", newTimestamp),
	)
	return child, nil
}

func (p *postForkCommonComponents) getInnerBlk() snowman.Block {
	return p.innerBlk
}

func (p *postForkCommonComponents) setInnerBlk(innerBlk snowman.Block) {
	p.innerBlk = innerBlk
}

func verifyIsOracleBlock(b snowman.Block) error {
	oracle, ok := b.(snowman.OracleBlock)
	if !ok {
		return fmt.Errorf(
			"%w: expected block %s to be a snowman.OracleBlock but it's a %T",
			errUnexpectedBlockType, b.ID(), b,
		)
	}
	_, err := oracle.Options()
	return err
}

func verifyIsNotOracleBlock(b snowman.Block) error {
	oracle, ok := b.(snowman.OracleBlock)
	if !ok {
		return nil
	}
	_, err := oracle.Options()
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
