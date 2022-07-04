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
	_ Block    = &AbortBlock{}
	_ Decision = &AbortBlock{}
)

// AbortBlock being accepted results in the proposal of its parent (which must
// be a proposal block) being rejected.
type AbortBlock struct {
	*stateless.AbortBlock
	*doubleDecisionBlock

	wasPreferred bool
}

// NewAbortBlock returns a new *AbortBlock where the block's parent, a proposal
// block, has ID [parentID]. Additionally the block will track if it was
// originally preferred or not for metrics.
func NewAbortBlock(
	verifier Verifier,
	txExecutorBackend executor.Backend,
	parentID ids.ID,
	height uint64,
	wasPreferred bool,
) (*AbortBlock, error) {
	statelessBlk, err := stateless.NewAbortBlock(parentID, height)
	if err != nil {
		return nil, err
	}
	return toStatefulAbortBlock(statelessBlk, verifier, txExecutorBackend, wasPreferred, choices.Processing)
}

func toStatefulAbortBlock(
	statelessBlk *stateless.AbortBlock,
	verifier Verifier,
	txExecutorBackend executor.Backend,
	wasPreferred bool,
	status choices.Status,
) (*AbortBlock, error) {
	abort := &AbortBlock{
		AbortBlock: statelessBlk,
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

	return abort, nil
}

func (a *AbortBlock) Accept() error {
	if a.txExecutorBackend.Bootstrapped.GetValue() {
		if a.wasPreferred {
			a.verifier.MarkAcceptedOptionVote()
		} else {
			a.verifier.MarkRejectedOptionVote()
		}
	}

	if err := a.doubleDecisionBlock.acceptParent(); err != nil {
		return err
	}

	a.accept()
	a.verifier.AddStatelessBlock(a.AbortBlock, a.Status())
	if err := a.verifier.MarkAccepted(a.AbortBlock); err != nil {
		return fmt.Errorf("failed to accept accept option block %s: %w", a.ID(), err)
	}

	return a.doubleDecisionBlock.updateState()
}

func (a *AbortBlock) Reject() error {
	a.txExecutorBackend.Ctx.Log.Verbo(
		"Rejecting Abort Block %s at height %d with parent %s",
		a.ID(),
		a.Height(),
		a.Parent(),
	)

	defer a.reject()
	a.verifier.AddStatelessBlock(a.AbortBlock, a.Status())
	return a.verifier.Commit()
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptState if the verification passes.
func (a *AbortBlock) Verify() error {
	if err := a.verify(); err != nil {
		return err
	}

	parentIntf, err := a.parentBlock()
	if err != nil {
		return err
	}

	// The parent of an Abort block should always be a proposal
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}

	a.onAcceptState = parent.onAbortState
	a.timestamp = a.onAcceptState.GetTimestamp()

	a.verifier.CacheVerifiedBlock(a)
	parent.addChild(a)
	return nil
}
