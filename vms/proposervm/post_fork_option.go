// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ Block = &postForkOption{}

// The parent of a *postForkOption must be a *postForkBlock.
type postForkOption struct {
	block.Block
	postForkCommonComponents
}

func (b *postForkOption) Timestamp() time.Time {
	// A *postForkOption's timestamp is its parent's timestamp
	parentID := b.Parent()
	parent, err := b.vm.GetBlock(parentID)
	if err != nil {
		b.vm.ctx.Log.Error("Could not find option %v 's parent block %v to retrieve its timestamp", b.ID(), b.Parent())
	}
	return parent.Timestamp()
}

func (b *postForkOption) Accept() error {
	blkID := b.ID()
	if err := b.vm.State.SetLastAccepted(blkID); err != nil {
		return err
	}

	// Persist this block and its status
	b.status = choices.Accepted
	if err := b.vm.storePostForkBlock(b); err != nil {
		return err
	}

	delete(b.vm.verifiedBlocks, blkID)

	// mark the inner block as accepted and all conflicting inner blocks as
	// rejected
	return b.vm.Tree.Accept(b.innerBlk)
}

func (b *postForkOption) Reject() error {
	// we do not reject the inner block here because that block may be contained
	// in the proposer block that causing this block to be rejected.

	delete(b.vm.verifiedBlocks, b.ID())

	// Persist this block and its status
	b.status = choices.Rejected
	return b.vm.storePostForkBlock(b)
}

func (b *postForkOption) Status() choices.Status { return b.status }

func (b *postForkOption) Parent() ids.ID {
	return b.ParentID()
}

// If Verify returns nil, Accept or Reject is eventually called on [b] and
// [b.innerBlk].
func (b *postForkOption) Verify() error {
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
	// A *postForkOption's parent can't be a *postForkOption
	return errUnexpectedBlockType
}

func (b *postForkOption) buildChild() (Block, error) {
	parentPChainHeight, err := b.pChainHeight()
	if err != nil {
		return nil, err
	}
	return b.postForkCommonComponents.buildChild(
		b.ID(),
		b.Timestamp(),
		parentPChainHeight,
	)
}

// This block's P-Chain height is its parent's P-Chain height
func (b *postForkOption) pChainHeight() (uint64, error) {
	parent, err := b.vm.getBlock(b.ParentID())
	if err != nil {
		return 0, err
	}
	return parent.pChainHeight()
}

func (b *postForkOption) setStatus(status choices.Status) {
	b.status = status
}

func (b *postForkOption) getStatelessBlk() block.Block {
	return b.Block
}
