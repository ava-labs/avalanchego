// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/core"
)

// Commit being accepted results in the proposal of its parent (which must be a proposal block)
// being enacted.
type Commit struct {
	DoubleDecisionBlock `serialize:"true"`
}

// Verify this block performs a valid state transition.
//
// The parent block must either be a proposal
//
// This function also sets the onCommit databases if the verification passes.
func (c *Commit) Verify() error {
	// the parent of an Commit block should always be a proposal
	if parent, ok := c.parentBlock().(*ProposalBlock); ok {
		c.onAcceptDB, c.onAcceptFunc = parent.onCommit()
	} else {
		return errInvalidBlockType
	}

	c.vm.currentBlocks[c.ID().Key()] = c
	c.parentBlock().addChild(c)
	return nil
}

// newCommitBlock returns a new *Commit block where the block's parent, a
// proposal block, has ID [parentID].
func (vm *VM) newCommitBlock(parentID ids.ID) *Commit {
	commit := &Commit{DoubleDecisionBlock: DoubleDecisionBlock{CommonDecisionBlock: CommonDecisionBlock{CommonBlock: CommonBlock{
		Block: core.NewBlock(parentID),
		vm:    vm,
	}}}}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(commit)
	bytes, err := Codec.Marshal(&blk)
	if err != nil {
		return nil
	}
	commit.Block.Initialize(bytes, vm.SnowmanVM)
	return commit
}
