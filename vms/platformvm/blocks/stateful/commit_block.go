// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	_ Block    = &CommitBlock{}
	_ Decision = &CommitBlock{}
)

// CommitBlock being accepted results in the proposal of its parent (which must
// be a proposal block) being enacted.
type CommitBlock struct {
	*stateless.CommitBlock
	*doubleDecisionBlock

	wasPreferred bool
}

// NewCommitBlock returns a new *Commit block where the block's parent, a
// proposal block, has ID [parentID]. Additionally the block will track if it
// was originally preferred or not for metrics.
func NewCommitBlock(
	verifier Verifier,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	wasPreferred bool,
) (*CommitBlock, error) {
	statelessBlk, err := stateless.NewCommitBlock(parentID, height)
	if err != nil {
		return nil, err
	}

	return toStatefulCommitBlock(statelessBlk, verifier, txExecutorBackend, wasPreferred, choices.Processing)
}

func toStatefulCommitBlock(
	statelessBlk *stateless.CommitBlock,
	verifier Verifier,
	txExecutorBackend executor.Backend,
	wasPreferred bool,
	status choices.Status,
) (*CommitBlock, error) {
	commit := &CommitBlock{
		CommitBlock: statelessBlk,
		doubleDecisionBlock: &doubleDecisionBlock{
			decisionBlock: decisionBlock{
				commonBlock: &commonBlock{
					baseBlk:           &statelessBlk.CommonBlock,
					status:            status,
					verifier:          verifier,
					txExecutorBackend: txExecutorBackend,
				},
			},
		},
		wasPreferred: wasPreferred,
	}

	return commit, nil
}

func (c *CommitBlock) Accept() error {
	if c.txExecutorBackend.Bootstrapped.GetValue() {
		if c.wasPreferred {
			c.verifier.MarkAcceptedOptionVote()
		} else {
			c.verifier.MarkRejectedOptionVote()
		}
	}

	if err := c.doubleDecisionBlock.acceptParent(); err != nil {
		return err
	}

	c.accept()
	c.verifier.AddStatelessBlock(c.CommitBlock, c.Status())
	if err := c.verifier.MarkAccepted(c.CommitBlock); err != nil {
		return fmt.Errorf("failed to accept commit block %s: %w", c.ID(), err)
	}

	return c.doubleDecisionBlock.updateState()
}

func (c *CommitBlock) Reject() error {
	c.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting CommitBlock Block %s at height %d with parent %s",
		c.ID(), c.Height(), c.Parent(),
	)

	defer c.reject()
	c.verifier.AddStatelessBlock(c.CommitBlock, c.Status())
	return c.verifier.Commit()
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptState if the verification passes.
func (c *CommitBlock) Verify() error {
	if err := c.verify(); err != nil {
		return err
	}

	parentIntf, err := c.parentBlock()
	if err != nil {
		return err
	}

	// The parent of a Commit block should always be a proposal
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}

	c.onAcceptState = parent.onCommitState
	c.timestamp = c.onAcceptState.GetTimestamp()

	c.verifier.CacheVerifiedBlock(c)
	parent.addChild(c)
	return nil
}
