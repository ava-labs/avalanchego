// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"time"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/option"
)

var _ Block = &postForkOption{}

// The parent of a *postForkOption must be a *postForkBlock.
type postForkOption struct {
	option.Option
	postForkCommonComponents
}

func (b *postForkOption) Timestamp() time.Time {
	// A *postForkOption's timestamp is its parent's timestamp
	parent := b.Parent()
	return parent.Timestamp()
}

func (b *postForkOption) Accept() error {
	b.status = choices.Accepted
	blkID := b.ID()
	if err := b.vm.State.SetLastAccepted(blkID); err != nil {
		return err
	}

	// Persist this block and its status
	if err := b.vm.storePostForkOption(b); err != nil {
		return err
	}

	// mark the inner block as accepted and all conflicting inner blocks as rejected
	if err := b.vm.Tree.Accept(b.innerBlk); err != nil {
		return err
	}

	delete(b.vm.verifiedBlocks, blkID)
	return nil
}

func (b *postForkOption) Reject() error {
	// we do not reject the inner block here because that block may be contained
	// in the proposer block that causing this block to be rejected.
	b.status = choices.Rejected

	// Persist this block and its status
	if err := b.vm.storePostForkOption(b); err != nil {
		return err
	}

	delete(b.vm.verifiedBlocks, b.ID())
	return nil
}

func (b *postForkOption) Status() choices.Status { return b.status }

func (b *postForkOption) Parent() snowman.Block {
	parentID := b.ParentID()
	res, err := b.vm.getBlock(parentID)
	if err != nil {
		return &missing.Block{BlkID: parentID}
	}
	return res
}

// If Verify returns nil, Accept or Reject is eventually called on
// [b] a
func (b *postForkOption) Verify() error {
	b.vm.ctx.Log.Debug("Snowman++ calling verify on %s", b.ID())

	parent, err := b.vm.getBlock(b.ParentID())
	if err != nil {
		return err
	}
	return parent.verifyPostForkOption(b)
}

func (b *postForkOption) getInnerBlk() snowman.Block {
	return b.innerBlk
}

func (b *postForkOption) verifyPreForkChild(child *preForkBlock) error {
	// A *preForkBlock's parent must be a *preForkBlock
	return errUnsignedChild
}

func (b *postForkOption) verifyPostForkChild(child *postForkBlock) error {
	parentTimestamp := b.Timestamp()
	parentPChainHeight, err := b.pChainHeight()
	if err != nil {
		return err
	}
	return b.postForkCommonComponents.Verify(
		parentTimestamp,
		parentPChainHeight,
		child,
	)
}

func (b *postForkOption) verifyPostForkOption(child *postForkOption) error {
	// A *postForkOption's parent cant be a *postForkOption
	return errUnexpectedBlockType
}

func (b *postForkOption) buildChild(innerBlock snowman.Block) (Block, error) {
	parentID := b.ID()
	parentTimestamp := b.Timestamp()
	newTimestamp := b.vm.Time().Truncate(time.Second)
	// The child's timestamp is the later of now and this block's timestamp
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
	}

	// The following [minTimestamp] check should be able to be removed, but this
	// is left here as a sanity check
	childHeight := innerBlock.Height()
	parentPChainHeight, err := b.pChainHeight()
	if err != nil {
		return nil, err
	}

	proposerID := b.vm.ctx.NodeID
	minDelay, err := b.vm.Windower.Delay(childHeight, parentPChainHeight, proposerID)
	if err != nil {
		return nil, err
	}

	minTimestamp := parentTimestamp.Add(minDelay)
	if newTimestamp.Before(minTimestamp) {
		// It's not our turn to propose a block yet
		b.vm.ctx.Log.Warn("Snowman++ build post-fork option - dropped option; parent timestamp %s, expected delay %s, block timestamp %s.",
			parentTimestamp, minDelay, newTimestamp)
		return nil, errProposerWindowNotStarted
	}

	// The child's P-Chain height is the P-Chain's height when it
	// was proposed (i.e. now)
	pChainHeight, err := b.vm.PChainHeight()
	if err != nil {
		return nil, err
	}

	// Build the child
	statelessChild, err := block.Build(
		parentID,
		newTimestamp,
		pChainHeight,
		b.vm.ctx.StakingCertLeaf,
		innerBlock.Bytes(),
		b.vm.ctx.StakingLeafSigner,
	)
	if err != nil {
		return nil, err
	}

	child := &postForkBlock{
		Block: statelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       b.vm,
			innerBlk: innerBlock,
			status:   choices.Processing,
		},
	}

	b.vm.ctx.Log.Debug("Snowman++ build post-fork option %s - parent timestamp %v, expected delay %v, block timestamp %v.",
		child.ID(), parentTimestamp, minDelay, newTimestamp)
	// Persist the child
	return child, b.vm.storePostForkBlock(child)
}

// This block's P-Chain height is its parent's P-Chain height
func (b *postForkOption) pChainHeight() (uint64, error) {
	parent, err := b.vm.getBlock(b.ParentID())
	if err != nil {
		return 0, err
	}
	return parent.pChainHeight()
}
