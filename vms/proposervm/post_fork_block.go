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
	// Persist this block with its status
	if err := b.vm.storePostForkBlock(b); err != nil {
		return err
	}

	// mark the inner block as accepted and all conflicting inner blocks as rejected
	if err := b.vm.Tree.Accept(b.innerBlk); err != nil {
		return err
	}

	delete(b.vm.verifiedBlocks, blkID)
	return nil
}

func (b *postForkBlock) Reject() error {
	// We do not reject the inner block here because it
	// may be accepted later
	b.status = choices.Rejected
	// Persist this block with its status
	if err := b.vm.storePostForkBlock(b); err != nil {
		return err
	}

	delete(b.vm.verifiedBlocks, b.ID())
	return nil
}

func (b *postForkBlock) Status() choices.Status { return b.status }

// Return this block's parent, or a *missing.Block if
// we don't have the parent.
func (b *postForkBlock) Parent() snowman.Block {
	parentID := b.ParentID()
	res, err := b.vm.getBlock(parentID)
	if err != nil {
		return &missing.Block{BlkID: parentID}
	}
	return res
}

// If Verify() returns nil, Accept() or Reject() will eventually be called
// on [b] and [b.innerBlk]
func (b *postForkBlock) Verify() error {
	b.vm.ctx.Log.Debug("Snowman++ calling verify on %s", b.ID())

	parent, err := b.vm.getBlock(b.ParentID())
	if err != nil {
		return err
	}
	return parent.verifyPostForkChild(b)
}

// Return the two options for the block that follows [b]
func (b *postForkBlock) Options() ([2]snowman.Block, error) {
	innerOracleBlk, ok := b.innerBlk.(snowman.OracleBlock)
	if !ok {
		// [b]'s innerBlk isn't an oracle block
		return [2]snowman.Block{}, snowman.ErrNotOracle
	}

	// The inner block's child options
	innerOptions, err := innerOracleBlk.Options()
	if err != nil {
		return [2]snowman.Block{}, err
	}

	parentID := b.ID()
	outerOptions := [2]snowman.Block{}
	for i, innerOption := range innerOptions {
		// Wrap the inner block's child option
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
		// Persist the wrapped child options
		if err := b.vm.storePostForkOption(outerOption); err != nil {
			return [2]snowman.Block{}, err
		}

		outerOptions[i] = outerOption
	}
	return outerOptions, nil
}

// A post-fork block can never have a pre-fork child
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
		b.vm.ctx.Log.Debug("post-fork option block's parent does not have an oracle parent")
		return errUnexpectedBlockType
	}

	// Make sure [b]'s inner block is the parent of [child]'s inner block
	expectedInnerParentID := b.innerBlk.ID()
	innerParent := child.innerBlk.Parent()
	innerParentID := innerParent.ID()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	return child.innerBlk.Verify()
}

// Return the child (a *postForkBlock) of this block
func (b *postForkBlock) buildChild(innerBlock snowman.Block) (Block, error) {
	parentID := b.ID()
	parentTimestamp := b.Timestamp()
	// Child's timestamp is the later of now and this block's timestamp
	newTimestamp := b.vm.Time().Truncate(time.Second)
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
		// It's not our turn to propose a block yet
		b.vm.ctx.Log.Debug("Snowman++ build post-fork block - parent timestamp %s, expected delay %s, block timestamp %s. Dropping block, build called too early.",
			parentTimestamp.Format("15:04:05"), minDelay, newTimestamp.Format("15:04:05"))
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

	b.vm.ctx.Log.Debug("Snowman++ build post-fork block %s - parent timestamp %v, expected delay %v, block timestamp %v.",
		child.ID(), parentTimestamp.Format("15:04:05"), minDelay, newTimestamp.Format("15:04:05"))
	// Persist the child
	return child, b.vm.storePostForkBlock(child)
}

func (b *postForkBlock) pChainHeight() (uint64, error) {
	return b.PChainHeight(), nil
}
