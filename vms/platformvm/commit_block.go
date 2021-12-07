// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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

	wasPreferred bool
}

func (c *CommitBlock) Accept() error {
	if c.vm.bootstrapped.GetValue() {
		if c.wasPreferred {
			c.vm.metrics.numVotesWon.Inc()
		} else {
			c.vm.metrics.numVotesLost.Inc()
		}
	}
	return c.DoubleDecisionBlock.Accept()
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptState if the verification passes.
func (c *CommitBlock) Verify() error {
	blkID := c.ID()

	if err := c.DoubleDecisionBlock.Verify(); err != nil {
		return err
	}

	parentIntf, err := c.parentBlock()
	if err != nil {
		return err
	}

	// The parent of a Commit block should always be a proposal
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		return errInvalidBlockType
	}

	c.onAcceptState = parent.onCommitState
	c.timestamp = c.onAcceptState.GetTimestamp()

	c.vm.currentBlocks[blkID] = c
	parent.addChild(c)
	return nil
}

// newCommitBlock returns a new *Commit block where the block's parent, a
// proposal block, has ID [parentID]. Additionally the block will track if it
// was originally preferred or not for metrics.
func (vm *VM) newCommitBlock(parentID ids.ID, height uint64, wasPreferred bool) (*CommitBlock, error) {
	commit := &CommitBlock{
		DoubleDecisionBlock: DoubleDecisionBlock{
			CommonDecisionBlock: CommonDecisionBlock{
				CommonBlock: CommonBlock{
					PrntID: parentID,
					Hght:   height,
				},
			},
		},
		wasPreferred: wasPreferred,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(commit)
	bytes, err := Codec.Marshal(CodecVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal commit block: %w", err)
	}
	return commit, commit.DoubleDecisionBlock.initialize(vm, bytes, choices.Processing, commit)
}
