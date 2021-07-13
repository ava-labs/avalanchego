// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"
	"time"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/option"
)

var _ Block = &postForkOption{}

type postForkOption struct {
	option.Option
	postForkCommonComponents
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
	parentPChainHeight, err := b.pChainHeight()
	if err != nil {
		return err
	}
	return postForkCommonVerify(&b.postForkCommonComponents, b.Timestamp(),
		parentPChainHeight, child)
}

func (b *postForkOption) verifyPostForkOption(child *postForkOption) error {
	return errUnexpectedBlockType
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
		Block: statelessBlock,
		postForkCommonComponents: postForkCommonComponents{
			vm:       b.vm,
			innerBlk: innerBlock,
			status:   choices.Processing,
		},
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
