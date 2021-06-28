// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var (
	_ Block    = &CommitBlock{}
	_ decision = &CommitBlock{}
)

// CommitBlock being accepted results in the proposal of its parent (which must
// be a proposal block) being enacted.
type CommitBlock struct {
	DoubleDecisionBlock `serialize:"true"`
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptState if the verification passes.
func (c *CommitBlock) Verify() error {
	blkID := c.ID()

	if err := c.DoubleDecisionBlock.Verify(); err != nil {
		c.vm.ctx.Log.Trace("rejecting block %s due to a failed verification: %s", blkID, err)
		if err := c.Reject(); err != nil {
			c.vm.ctx.Log.Error("failed to reject commit block %s due to %s", blkID, err)
		}
		return err
	}

	parentIntf, err := c.parent()
	if err != nil {
		return err
	}

	// The parent of a Commit block should always be a proposal
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		c.vm.ctx.Log.Trace("rejecting block %s due to an incorrect parent type", blkID)
		if err := c.Reject(); err != nil {
			c.vm.ctx.Log.Error("failed to reject commit block %s due to %s", blkID, err)
		}
		return errInvalidBlockType
	}

	c.onAcceptState, c.onAcceptFunc = parent.onCommit()

	c.vm.currentBlocks[blkID] = c
	parent.addChild(c)
	return nil
}

// newCommitBlock returns a new *Commit block where the block's parent, a
// proposal block, has ID [parentID].
func (vm *VM) newCommitBlock(parentID ids.ID, height uint64) (*CommitBlock, error) {
	commit := &CommitBlock{
		DoubleDecisionBlock: DoubleDecisionBlock{
			CommonDecisionBlock: CommonDecisionBlock{
				CommonBlock: CommonBlock{
					PrntID: parentID,
					Hght:   height,
				},
			},
		},
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(commit)
	bytes, err := Codec.Marshal(codecVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal commit block: %w", err)
	}
	return commit, commit.DoubleDecisionBlock.initialize(vm, bytes, choices.Processing, commit)
}
