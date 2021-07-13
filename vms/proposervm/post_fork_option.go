// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/option"
)

var _ Block = &postForkOption{}

type postForkOption struct {
	option.Option

	vm       *VM
	innerBlk snowman.Block
	status   choices.Status
}

func (b *postForkOption) Timestamp() time.Time {
	parent := b.Parent()
	return parent.Timestamp()
}

func (b *postForkOption) Accept() error {
	b.status = choices.Accepted
	blkID := b.ID()
	if err := b.vm.State.SetLastAccepted(blkID); err != nil {
		return err
	}

	if err := b.vm.storePostForkOption(b); err != nil {
		return err
	}

	// mark the inner block as accepted and all the conflicting inner blocks as
	// rejected
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

func (b *postForkOption) Verify() error {
	b.vm.ctx.Log.Debug("Snowman++ calling verify on %s", b.ID())

	parent, err := b.vm.getBlock(b.ParentID())
	if err != nil {
		return err
	}
	return parent.verifyPostForkOption(b)
}

func (b *postForkOption) Height() uint64 {
	return b.innerBlk.Height()
}

func (b *postForkOption) Options() ([2]snowman.Block, error) {
	// option block cannot be oracle itself
	return [2]snowman.Block{}, snowman.ErrNotOracle
}

func (b *postForkOption) verifyPreForkChild(child *preForkBlock) error {
	return errUnsignedChild
}

func (b *postForkOption) verifyPostForkChild(child *postForkBlock) error {
	// retrieve relevant data from parent's parent, which must be a postForkBlock
	parentPChainHeight, err := b.pChainHeight()
	if err != nil {
		return err
	}

	childPChainHeight := child.PChainHeight()
	if childPChainHeight < parentPChainHeight {
		return errPChainHeightNotMonotonic
	}

	currentPChainHeight, err := b.vm.PChainHeight()
	if err != nil {
		b.vm.ctx.Log.Error("Snowman++ verify post-fork block %s - could not retrieve current P-Chain height",
			child.ID())
	}
	if childPChainHeight > currentPChainHeight {
		return errPChainHeightNotReached
	}

	expectedInnerParentID := b.innerBlk.ID()
	innerParent := child.innerBlk.Parent()
	innerParentID := innerParent.ID()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	parentTimestamp := b.Timestamp()
	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	maxTimestamp := b.vm.Time().Add(syncBound)
	if childTimestamp.After(maxTimestamp) {
		return errTimeTooAdvanced
	}

	childHeight := child.Height()
	proposerID, err := b.proposer()
	if err != nil {
		return err
	}
	minDelay, err := b.vm.Windower.Delay(childHeight, parentPChainHeight, proposerID)
	if err != nil {
		return err
	}

	minTimestamp := parentTimestamp.Add(minDelay)
	b.vm.ctx.Log.Debug("Snowman++ verify post-fork block %s - parent timestamp %v, expected delay %v, block timestamp %v.",
		child.ID(), parentTimestamp.Format("15:04:05"), minDelay, childTimestamp.Format("15:04:05"))

	if childTimestamp.Before(minTimestamp) {
		b.vm.ctx.Log.Debug("Snowman++ verify - dropped post-fork block due to time window %s", child.ID())
		return errProposerWindowNotStarted
	}

	// Verify the signature of the node
	if err := child.Block.Verify(); err != nil {
		return err
	}

	// only validate the inner block once
	if !b.vm.Tree.Contains(child.innerBlk) {
		if err := child.innerBlk.Verify(); err != nil {
			return err
		}
		b.vm.Tree.Add(child.innerBlk)
	}

	b.vm.verifiedBlocks[child.ID()] = child
	return nil
}

func (b *postForkOption) verifyPostForkOption(child *postForkOption) error {
	if _, ok := b.innerBlk.(snowman.OracleBlock); !ok {
		return fmt.Errorf("post fork option block does has non-oracle father")
	}

	return child.innerBlk.Verify()
}

func (b *postForkOption) buildChild(innerBlock snowman.Block) (Block, error) {
	parentID := b.ID()
	parentTimestamp := b.Timestamp()
	newTimestamp := b.vm.Time()
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
	}

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
		b.vm.ctx.Log.Debug("Snowman++ build post-fork option - parent timestamp %v, expected delay %v, block timestamp %v. Dropping block, build called too early.",
			parentTimestamp.Format("15:04:05"), minDelay, newTimestamp.Format("15:04:05"))
		return nil, errProposerWindowNotStarted
	}

	pChainHeight, err := b.vm.ctx.ValidatorVM.GetCurrentHeight()
	if err != nil {
		return nil, err
	}

	statelessBlock, err := block.Build(
		parentID,
		newTimestamp,
		pChainHeight,
		b.vm.ctx.StakingCert.Leaf,
		innerBlock.Bytes(),
		b.vm.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		return nil, err
	}

	blk := &postForkBlock{
		Block:    statelessBlock,
		vm:       b.vm,
		innerBlk: innerBlock,
		status:   choices.Processing,
	}

	b.vm.ctx.Log.Debug("Snowman++ build post-fork option %s - parent timestamp %v, expected delay %v, block timestamp %v.",
		blk.ID(), parentTimestamp.Format("15:04:05"), minDelay, newTimestamp.Format("15:04:05"))

	return blk, b.vm.storePostForkBlock(blk)
}

func (b *postForkOption) pChainHeight() (uint64, error) {
	parent := b.Parent()
	pFBlk, ok := parent.(*postForkBlock)
	if !ok {
		b.vm.ctx.Log.Error("post-fork option parent is not post-fork block")
		return 0, errUnexpectedBlockType
	}

	return pFBlk.PChainHeight(), nil
}

func (b *postForkOption) proposer() (ids.ShortID, error) {
	parent := b.Parent()
	pFBlk, ok := parent.(*postForkBlock)
	if !ok {
		b.vm.ctx.Log.Error("post-fork option parent is not post-fork block")
		return ids.ShortID{}, errUnexpectedBlockType
	}

	return pFBlk.Proposer(), nil
}
