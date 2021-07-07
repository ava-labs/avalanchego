// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
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
	// TODO: check that option block cannot be an oracle itself
	return [2]snowman.Block{}, snowman.ErrNotOracle
}

func (b *postForkOption) verifyPreForkChild(child *preForkBlock) error {
	return errUnsignedChild
}

func (b *postForkOption) verifyPostForkChild(child *postForkBlock) error {
	// retrieve relevant data from parent's parent, which must be a postForkBlock
	parentParent := b.Parent()
	oracleBlk, ok := parentParent.(*postForkBlock)
	if !ok {
		return fmt.Errorf("expected postFork block as OracleBlock") // TODO: add error
	}

	parentPChainHeight := oracleBlk.PChainHeight()
	// TODO: verify that [childPChainHeight] is <= the P-chain's current height
	childPChainHeight := child.PChainHeight()
	if childPChainHeight < parentPChainHeight {
		return errPChainHeightNotMonotonic
	}

	expectedInnerParentID := b.innerBlk.ID()
	innerParent := child.innerBlk.Parent()
	innerParentID := innerParent.ID()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	parentTimestamp := oracleBlk.Timestamp()
	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	maxTimestamp := b.vm.Time().Add(syncBound)
	if childTimestamp.After(maxTimestamp) {
		return errTimeTooAdvanced
	}

	childHeight := child.Height()
	proposerID := oracleBlk.Proposer()
	minDelay, err := b.vm.Windower.Delay(childHeight, parentPChainHeight, proposerID)
	if err != nil {
		return err
	}

	minTimestamp := parentTimestamp.Add(minDelay)
	if childTimestamp.Before(minTimestamp) {
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
	option, err := option.Build(
		parentID,
		innerBlock.Bytes(),
	)
	if err != nil {
		return nil, err
	}

	blk := &postForkOption{
		Option:   option,
		vm:       b.vm,
		innerBlk: innerBlock,
		status:   choices.Processing,
	}

	return blk, b.vm.storePostForkOption(b)
}
