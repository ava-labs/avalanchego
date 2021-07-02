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

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ Block = &preForkBlock{}

type preForkBlock struct {
	snowman.Block
	vm *VM
}

// snowman.Block interface implementation
func (b *preForkBlock) Parent() snowman.Block {
	return &preForkBlock{
		Block: b.Block.Parent(),
		vm:    b.vm,
	}
}

func (b *preForkBlock) Verify() error {
	parent, err := b.vm.getBlock(b.Block.Parent().ID())
	if err != nil {
		return err
	}
	return parent.verifyPreForkChild(b)
}

func (b *preForkBlock) Options() ([2]snowman.Block, error) {
	if oracleBlk, ok := b.Block.(snowman.OracleBlock); ok {
		// TODO: correctly wrap the oracle blocks
		return oracleBlk.Options()
	}
	return [2]snowman.Block{}, snowman.ErrNotOracle
}

func (b *preForkBlock) verifyPreForkChild(child *preForkBlock) error {
	parentTimestamp := b.Timestamp()
	if !parentTimestamp.Before(b.vm.activationTime) {
		return errProposersActivated
	}

	return child.Block.Verify()
}

func (b *preForkBlock) verifyPostForkChild(child *postForkBlock) error {
	// TODO: verify that [childPChainHeight] is <= the P-chain's current height
	childPChainHeight := child.PChainHeight()
	_ = childPChainHeight

	expectedInnerParentID := b.ID()
	innerParent := child.innerBlk.Parent()
	innerParentID := innerParent.ID()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	// This block is expected to be the fork block
	parentTimestamp := b.Timestamp()
	if parentTimestamp.Before(b.vm.activationTime) {
		return errProposersNotActivated
	}

	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	maxTimestamp := b.vm.Time().Add(syncBound)
	if childTimestamp.After(maxTimestamp) {
		return errTimeTooAdvanced
	}

	// Verify the signature of the node
	if err := child.Block.Verify(); err != nil {
		return err
	}

	// validate core block, only once
	if !b.vm.Tree.Contains(child.innerBlk) {
		if err := child.innerBlk.Verify(); err != nil {
			return err
		}
		b.vm.Tree.Add(child.innerBlk)
	}

	b.vm.verifiedBlocks[child.ID()] = child
	return nil
}

func (b *preForkBlock) buildChild(innerBlock snowman.Block) (Block, error) {
	parentTimestamp := b.Timestamp()
	if parentTimestamp.Before(b.vm.activationTime) {
		// The chain hasn't forked yet
		return &preForkBlock{
			Block: innerBlock,
			vm:    b.vm,
		}, nil
	}

	// The chain is currently forking

	parentID := b.ID()
	newTimestamp := b.vm.Time()
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
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
