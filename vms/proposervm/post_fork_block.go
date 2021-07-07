// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

// ProposerBlock is a decorator for a snowman.Block, created to handle block headers introduced with snowman++
// ProposerBlock is made up of a ProposerBlockHeader, carrying all the new fields introduced with snowman++, and
// a core block, which is a snowman.Block.
// ProposerBlock serialization is a two step process: the header is serialized at proposervm level, while core block
// serialization is deferred to the core VM. The structure marshallingProposerBLock encapsulates
// the serialization logic
// Contract:
// * Parent ProposerBlock wraps Parent CoreBlock of CoreBlock wrapped into Child ProposerBlock.
// * Only one call to each coreBlock's Verify() is issued from proposerVM. However Verify is memory only, so we won't persist
// core blocks over which Verify has been called
// * VERIFY FAILS ON GENESIS TODO: fix maybe
// * Rejection of ProposerBlock does not constitute Rejection of wrapped CoreBlock

import (
	"crypto"
	"fmt"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ Block = &postForkBlock{}

// postForkBlock implements proposervm.Block
type postForkBlock struct {
	block.Block

	vm       *VM
	innerBlk snowman.Block
	status   choices.Status
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
	// in the proposer block that causing this block to be rejected.
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
	if oracleBlk, ok := b.innerBlk.(snowman.OracleBlock); ok {
		// TODO: correctly wrap the oracle blocks
		return oracleBlk.Options()
	}
	return [2]snowman.Block{}, snowman.ErrNotOracle
}

func (b *postForkBlock) verifyPreForkChild(child *preForkBlock) error {
	return errUnsignedChild
}

func (b *postForkBlock) verifyPostForkChild(child *postForkBlock) error {
	parentPChainHeight := b.PChainHeight()
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
	proposerID := b.Proposer()
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

func (b *postForkBlock) verifyPostForkOption(child *postForkOption) error {
	if _, ok := b.innerBlk.(snowman.OracleBlock); !ok {
		return fmt.Errorf("post fork option block does has non-oracle father")
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

	childHeight := innerBlock.Height()
	parentPChainHeight := b.PChainHeight()
	proposerID := b.vm.ctx.NodeID
	minDelay, err := b.vm.Windower.Delay(childHeight, parentPChainHeight, proposerID)
	if err != nil {
		return nil, err
	}

	minTimestamp := parentTimestamp.Add(minDelay)
	if newTimestamp.Before(minTimestamp) {
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
	return blk, b.vm.storePostForkBlock(blk)
}
