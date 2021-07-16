// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/option"
)

var _ Block = &postForkBlock{}

// postForkBlock implements proposervm.Block
type postForkBlock struct {
	block.Block
	postForkCommonComponents
}

func (b *postForkBlock) Accept() error {
	b.status = choices.Accepted
	blkID := b.ID()
	if err := b.vm.State.SetLastAccepted(blkID); err != nil {
		return err
	}
	if err := b.vm.storePostForkBlock(b); err != nil {
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

func (b *postForkBlock) Reject() error {
	// we do not reject the inner block here because that block may be contained
	// in the proposer block that is causing this block to be rejected.
	b.status = choices.Rejected
	if err := b.vm.storePostForkBlock(b); err != nil {
		return err
	}

	delete(b.vm.verifiedBlocks, b.ID())
	return nil
}

func (b *postForkBlock) Status() choices.Status { return b.status }

func (b *postForkBlock) Parent() snowman.Block {
	parentID := b.ParentID()
	res, err := b.vm.getBlock(parentID)
	if err != nil {
		return &missing.Block{BlkID: parentID}
	}
	return res
}

func (b *postForkBlock) Verify() error {
	b.vm.ctx.Log.Debug("Snowman++ calling verify on %s", b.ID())

	parent, err := b.vm.getBlock(b.ParentID())
	if err != nil {
		return err
	}
	return parent.verifyPostForkChild(b)
}

func (b *postForkBlock) Height() uint64 {
	return b.innerBlk.Height()
}

func (b *postForkBlock) Options() ([2]snowman.Block, error) {
	innerOracleBlk, ok := b.innerBlk.(snowman.OracleBlock)
	if !ok {
		return [2]snowman.Block{}, snowman.ErrNotOracle
	}

	innerOptions, err := innerOracleBlk.Options()
	if err != nil {
		return [2]snowman.Block{}, err
	}

	parentID := b.ID()
	outerOptions := [2]snowman.Block{}
	for i, innerOption := range innerOptions {
		statelessOuterOption, err := option.Build(
			parentID,
			innerOption.Bytes(),
		)
		if err != nil {
			return [2]snowman.Block{}, err
		}

		outerOption := &postForkOption{
			Option: statelessOuterOption,
			postForkCommonComponents: postForkCommonComponents{
				vm:       b.vm,
				innerBlk: innerOption,
				status:   innerOption.Status(),
			},
		}
		if err := b.vm.storePostForkOption(outerOption); err != nil {
			return [2]snowman.Block{}, err
		}

		outerOptions[i] = outerOption
	}
	return outerOptions, nil
}

func (b *postForkBlock) verifyPreForkChild(child *preForkBlock) error {
	return errUnsignedChild
}

func (b *postForkBlock) verifyPostForkChild(child *postForkBlock) error {
	parentTimestamp := b.Timestamp()
	parentPChainHeight := b.PChainHeight()
	return b.postForkCommonComponents.Verify(
		parentTimestamp,
		parentPChainHeight,
		child,
	)
}

func (b *postForkBlock) verifyPostForkOption(child *postForkOption) error {
	if _, ok := b.innerBlk.(snowman.OracleBlock); !ok {
		b.vm.ctx.Log.Debug("post fork option block does not have oracle father")
		return errUnexpectedBlockType
	}

	return child.innerBlk.Verify()
}

func (b *postForkBlock) buildChild(innerBlock snowman.Block) (Block, error) {
	parentID := b.ID()
	parentTimestamp := b.Timestamp()
	newTimestamp := b.vm.Time()
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
	}

	// The following [minTimestamp] check should be able to be removed, but this
	// is left here as a sanity check
	childHeight := innerBlock.Height()
	parentPChainHeight := b.PChainHeight()
	proposerID := b.vm.ctx.NodeID
	minDelay, err := b.vm.Windower.Delay(childHeight, parentPChainHeight, proposerID)
	if err != nil {
		return nil, err
	}

	minTimestamp := parentTimestamp.Add(minDelay)
	if newTimestamp.Before(minTimestamp) {
		b.vm.ctx.Log.Debug("Snowman++ build post-fork block - parent timestamp %v, expected delay %v, block timestamp %v. Dropping block, build called too early.",
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

	b.vm.ctx.Log.Debug("Snowman++ build post-fork block %s - parent timestamp %v, expected delay %v, block timestamp %v.",
		blk.ID(), parentTimestamp.Format("15:04:05"), minDelay, newTimestamp.Format("15:04:05"))
	return blk, b.vm.storePostForkBlock(blk)
}

func (b *postForkBlock) pChainHeight() (uint64, error) {
	return b.PChainHeight(), nil
}
